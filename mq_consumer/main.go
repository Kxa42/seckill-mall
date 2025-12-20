package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"seckill-mall/common/config"
	"strings"
)

const (
	MQ_URL         = "amqp://guest:guest@localhost:5672/"
	OrderQueue     = "orders"       //正常队列
	DeadExchange   = "dlx_exchange" //死信交换机
	DeadQueue      = "dead_queue"   //死信队列
	DeadRoutingKey = "dead_key"     //识别码
)

func setupQueue(ch *amqp.Channel) {
	//声明死信交换机
	err := ch.ExchangeDeclare(
		DeadExchange,
		"direct",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("无法声明死信交换机： %v", err)
	}

	//声明死信队列
	_, err = ch.QueueDeclare(
		DeadQueue,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("无法声明死信队列： %v", err)
	}

	//绑定死信队列到死信交换机
	err = ch.QueueBind(
		DeadQueue,
		DeadRoutingKey,
		DeadExchange,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("无法绑定死信队列： %v", err)
	}

	//声明主队列，并设置死信交换机参数
	args := amqp.Table{
		"x-dead-letter-exchange":    DeadExchange,   //指定死信交换机
		"x-dead-letter-routing-key": DeadRoutingKey, //指定死信路由键
	}

	_, err = ch.QueueDeclare(
		OrderQueue,
		true,
		false,
		false,
		false,
		args,
	)
	if err != nil {
		log.Fatalf("无法声明主队列： %v", err)
	}

	log.Printf("RabbitMQ 队列和交换机设置完成")
}

type OrderMessage struct {
	OrderID   string  `json:"order_id"`
	UserID    int64   `json:"user_id"`
	ProductID int64   `json:"product_id"`
	Amount    float32 `json:"amount"`
}

type Order struct {
	ID        int64     `gorm:"primaryKey"`
	OrderID   string    `gorm:"type:varchar(64)"`
	UserID    int64     `gorm:"type:bigint"`
	ProductID int64     `gorm:"type:bigint"`
	Amount    float32   `gorm:"type:decimal(10,2)"`
	Status    int32     `gorm:"type:int"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

func (Order) TableName() string {
	return "orders"
}

var db *gorm.DB

func main() {
	//加载配置并连接MySQL
	config.InitConfig()
	initDB()

	conn, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatal(err)
	}
	defer ch.Close()

	q, err := ch.QueueDeclare(
		"seckill_order_queue",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatal(err)
	}

	ch.Qos(1, 0, false)
	setupQueue(ch)
	msgs, err := ch.Consume(
		q.Name,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("消费者服务已启动，等待订单中...")

	forever := make(chan struct{})
	go func() {
		for d := range msgs {
			var msg OrderMessage
			json.Unmarshal(d.Body, &msg)
			fmt.Printf("接收到订单: %s | 金额：%.2f | 开始落库...", msg.OrderID, msg.Amount)

			order := Order{
				OrderID:   msg.OrderID,
				UserID:    msg.UserID,
				ProductID: msg.ProductID,
				Amount:    msg.Amount,
				Status:    1,
			}

			//模拟慢速数据库写入
			time.Sleep(50 * time.Millisecond) // 模拟落库延迟

			err = db.Create(&order).Error
			if err != nil {
				//引入判断是不是“重复主键”错误
				if strings.Contains(err.Error(), "Duplicate entry") {
					fmt.Println("订单 %s 已存在，忽略重复消费", order.OrderID)
					//任务已完成，返回Ack告知RabbitMQ
					d.Ack(false)
				} else {
					//可能的无网络或数据库挂了
					log.Printf("订单落库失败（暂存死信/重试）: %v", err)
					//重试次数限制暂缓实施
					d.Nack(false, false)
				}
			} else {
				fmt.Println("订单落库成功")

				//临时测试
				//log.Println("模拟网络延迟，还没发Ack...")
				//time.Sleep(10 * time.Second)

				d.Ack(false)
			}
		}
	}()

	<-forever
}

func initDB() {
	dsn := config.Conf.MySQL.DSN
	var err error
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("连接MySQL失败: %v", err)
	}
}

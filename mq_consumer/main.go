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
)

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
			fmt.Printf("接收到订单: %s | 金额：%。2f | 开始落库...", msg.OrderID, msg.Amount)

			order := Order{
				OrderID:   msg.OrderID,
				UserID:    msg.UserID,
				ProductID: msg.ProductID,
				Amount:    msg.Amount,
				Status:    1,
			}

			//模拟慢速数据库写入
			time.Sleep(50 * time.Millisecond) // 模拟落库延迟

			if err := db.Create(&order).Error; err != nil {
				log.Printf("订单落库失败: %v", err) //后续的Nack暂缓实施
			} else {
				fmt.Println("订单落库成功")
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

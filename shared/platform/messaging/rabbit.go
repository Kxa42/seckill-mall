package messaging

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"seckill-mall/shared/contracts"
	"seckill-mall/shared/platform/tracer"
)

const (
	ConsumerOrder       = contracts.ServiceOrder
	ConsumerPayment     = contracts.ServicePayment
	ConsumerInventory   = contracts.ServiceInventory
	ConsumerFulfillment = contracts.ServiceFulfillment
)

var consumerEvents = map[string][]string{
	ConsumerOrder: {
		contracts.EventSeckillAccepted, contracts.EventPaymentSucceeded, contracts.EventPaymentRefunded,
		contracts.EventInventoryReserved, contracts.EventInventoryReleased, contracts.EventShipmentCreated,
		contracts.EventShipmentDelivered,
	},
	ConsumerPayment:   {contracts.EventOrderCreated, contracts.EventOrderCancelled},
	ConsumerInventory: {contracts.EventOrderCancelled, contracts.EventPaymentRefunded},
	ConsumerFulfillment: {
		contracts.EventPaymentSucceeded,
	},
}

// RabbitPublisher 使用单 Channel 串行等待 confirm，避免 delivery tag 与并发发布错配。
type RabbitPublisher struct {
	url      string
	mu       sync.Mutex
	conn     *amqp.Connection
	channel  *amqp.Channel
	confirms <-chan amqp.Confirmation
	returns  <-chan amqp.Return
}

func NewRabbitPublisher(url string) *RabbitPublisher { return &RabbitPublisher{url: url} }

func (p *RabbitPublisher) Connect() error {
	p.close()
	if p.url == "" {
		return errors.New("rabbitmq url is required")
	}
	conn, err := amqp.Dial(p.url)
	if err != nil {
		return fmt.Errorf("rabbitmq connect: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("rabbitmq channel: %w", err)
	}
	if err := DeclareTopology(ch); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("rabbitmq confirm: %w", err)
	}
	p.conn, p.channel = conn, ch
	p.confirms = ch.NotifyPublish(make(chan amqp.Confirmation, 1))
	p.returns = ch.NotifyReturn(make(chan amqp.Return, 1))
	return nil
}

func (p *RabbitPublisher) Publish(ctx context.Context, event contracts.EventEnvelope, headers amqp.Table) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.publishLocked(ctx, event, headers); err == nil {
		return nil
	}
	if err := p.Connect(); err != nil {
		return err
	}
	return p.publishLocked(ctx, event, headers)
}

func (p *RabbitPublisher) publishLocked(ctx context.Context, event contracts.EventEnvelope, headers amqp.Table) error {
	if p.channel == nil {
		if err := p.Connect(); err != nil {
			return err
		}
	}
	body, err := MarshalEnvelope(event)
	if err != nil {
		return err
	}
	headers = tracer.InjectAMQPHeaders(ctx, headers)
	if err := p.channel.PublishWithContext(ctx, DefaultExchange, event.EventType, true, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    event.EventID,
		Type:         event.EventType,
		Timestamp:    event.OccurredAt,
		Headers:      headers,
		Body:         body,
	}); err != nil {
		return fmt.Errorf("rabbitmq publish: %w", err)
	}
	select {
	case confirmation, ok := <-p.confirms:
		if !ok {
			return errors.New("rabbitmq confirm channel closed")
		}
		if !confirmation.Ack {
			return fmt.Errorf("rabbitmq publish nack delivery_tag=%d", confirmation.DeliveryTag)
		}
		select {
		case returned := <-p.returns:
			return fmt.Errorf("rabbitmq message returned code=%d text=%s key=%s", returned.ReplyCode, returned.ReplyText, returned.RoutingKey)
		default:
			return nil
		}
	case <-ctx.Done():
		return fmt.Errorf("rabbitmq confirm wait: %w", ctx.Err())
	}
}

func (p *RabbitPublisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.close()
}

func (p *RabbitPublisher) close() error {
	p.confirms, p.returns = nil, nil
	if p.channel != nil {
		_ = p.channel.Close()
		p.channel = nil
	}
	if p.conn != nil {
		_ = p.conn.Close()
		p.conn = nil
	}
	return nil
}

// DeclareTopology 声明统一商城交换机、队列、retry 和 DLQ。
func DeclareTopology(ch *amqp.Channel) error {
	if ch == nil {
		return errors.New("rabbitmq channel is required")
	}
	if err := ch.ExchangeDeclare(DefaultExchange, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare event exchange: %w", err)
	}
	if err := ch.ExchangeDeclare(RetryExchange, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare retry exchange: %w", err)
	}
	if err := ch.ExchangeDeclare(DeadExchange, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dead exchange: %w", err)
	}
	for consumer, events := range consumerEvents {
		if err := declareConsumerTopology(ch, consumer, events); err != nil {
			return err
		}
	}
	return nil
}

func declareConsumerTopology(ch *amqp.Channel, consumer string, events []string) error {
	mainQueue := queueName(consumer)
	dlq := deadQueueName(consumer)
	retry := retryQueueName(consumer)
	if _, err := ch.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare %s dlq: %w", consumer, err)
	}
	if err := ch.QueueBind(dlq, consumer, DeadExchange, false, nil); err != nil {
		return fmt.Errorf("bind %s dlq: %w", consumer, err)
	}
	if _, err := ch.QueueDeclare(retry, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange":    "",
		"x-dead-letter-routing-key": mainQueue,
	}); err != nil {
		return fmt.Errorf("declare %s retry: %w", consumer, err)
	}
	if err := ch.QueueBind(retry, consumer, RetryExchange, false, nil); err != nil {
		return fmt.Errorf("bind %s retry: %w", consumer, err)
	}
	if _, err := ch.QueueDeclare(mainQueue, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange":    DeadExchange,
		"x-dead-letter-routing-key": consumer,
	}); err != nil {
		return fmt.Errorf("declare %s queue: %w", consumer, err)
	}
	for _, event := range events {
		if err := ch.QueueBind(mainQueue, event, DefaultExchange, false, nil); err != nil {
			return fmt.Errorf("bind %s event=%s: %w", consumer, event, err)
		}
	}
	return nil
}

// RabbitConsumer 消费一个消费者自己的主队列，处理逻辑由调用方注入。
type RabbitConsumer struct {
	url string
}

func NewRabbitConsumer(url string) *RabbitConsumer { return &RabbitConsumer{url: url} }

// Run 建立一次消费会话；连接断开时由服务入口重新调用。
func (c *RabbitConsumer) Run(ctx context.Context, consumer string, inbox InboxStore, handlers map[string]Handler, maxAttempts int) error {
	if c == nil || c.url == "" {
		return errors.New("rabbitmq consumer url is required")
	}
	if inbox == nil {
		return errors.New("rabbitmq consumer inbox is required")
	}
	conn, err := amqp.Dial(c.url)
	if err != nil {
		return fmt.Errorf("rabbitmq consumer connect: %w", err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("rabbitmq consumer channel: %w", err)
	}
	defer ch.Close()
	if err := DeclareTopology(ch); err != nil {
		return err
	}
	if err := ch.Qos(16, 0, false); err != nil {
		return fmt.Errorf("rabbitmq consumer qos: %w", err)
	}
	deliveries, err := ch.Consume(queueName(consumer), "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("rabbitmq consume: %w", err)
	}
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				return errors.New("rabbitmq delivery channel closed")
			}
			if err := c.handleDelivery(ctx, ch, consumer, inbox, handlers, maxAttempts, delivery); err != nil {
				log.Printf("message consumer delivery failed consumer=%s err=%v", consumer, err)
			}
		}
	}
}

func (c *RabbitConsumer) handleDelivery(ctx context.Context, ch *amqp.Channel, consumer string, inbox InboxStore, handlers map[string]Handler, maxAttempts int, delivery amqp.Delivery) error {
	event, err := UnmarshalEnvelope(delivery.Body)
	if err != nil {
		return delivery.Nack(false, false)
	}
	if !contracts.IsKnownEventType(event.EventType) {
		return delivery.Ack(false)
	}
	if event.EventVersion > 1 {
		return delivery.Ack(false)
	}
	handler, ok := handlers[event.EventType]
	if !ok {
		return delivery.Ack(false)
	}
	claim, err := inbox.Claim(ctx, consumer, event, 30*time.Second)
	if err != nil {
		return c.retryOrDeadLetter(ctx, ch, consumer, event, delivery, 1, maxAttempts, err)
	}
	if claim.Processed {
		return delivery.Ack(false)
	}
	if !claim.Claimed {
		return c.requeueDelayed(ctx, ch, consumer, event, delivery, claim.Attempts)
	}
	traceCtx := tracer.ExtractAMQPHeaders(ctx, delivery.Headers)
	if err := handler(traceCtx, event); err != nil {
		_ = inbox.MarkFailed(ctx, consumer, event.EventID, err.Error())
		return c.retryOrDeadLetter(ctx, ch, consumer, event, delivery, claim.Attempts, maxAttempts, err)
	}
	if err := inbox.MarkProcessed(ctx, consumer, event.EventID); err != nil {
		return c.retryOrDeadLetter(ctx, ch, consumer, event, delivery, claim.Attempts, maxAttempts, err)
	}
	return delivery.Ack(false)
}

func (c *RabbitConsumer) retryOrDeadLetter(ctx context.Context, ch *amqp.Channel, consumer string, event contracts.EventEnvelope, delivery amqp.Delivery, attempt, maxAttempts int, cause error) error {
	if attempt >= maxAttempts {
		return delivery.Nack(false, false)
	}
	return c.publishRetry(ctx, ch, consumer, event, delivery, attempt)
}

// requeueDelayed 在 Inbox 租约被其他副本持有时延迟重投，替代立即重排以消除忙循环。
// 该分支永不 DLQ：持有租约的副本可能正在成功处理，租约过期后 attempts 自然递增收敛。
func (c *RabbitConsumer) requeueDelayed(ctx context.Context, ch *amqp.Channel, consumer string, event contracts.EventEnvelope, delivery amqp.Delivery, attempt int) error {
	return c.publishRetry(ctx, ch, consumer, event, delivery, attempt)
}

// publishRetry 将事件发布到 RetryExchange 延迟重投并 Ack 原消息；发布失败时 Nack 拒绝。
func (c *RabbitConsumer) publishRetry(ctx context.Context, ch *amqp.Channel, consumer string, event contracts.EventEnvelope, delivery amqp.Delivery, attempt int) error {
	body, err := MarshalEnvelope(event)
	if err != nil {
		return delivery.Nack(false, false)
	}
	headers := amqp.Table{}
	for key, value := range delivery.Headers {
		headers[key] = value
	}
	headers["x-event-attempt"] = attempt + 1
	if err := ch.PublishWithContext(ctx, RetryExchange, consumer, false, false, amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, MessageId: event.EventID, Type: event.EventType, Headers: headers, Expiration: strconv.FormatInt(RetryDelay(attempt+1, time.Second).Milliseconds(), 10), Body: body}); err != nil {
		return delivery.Nack(false, false)
	}
	return delivery.Ack(false)
}

func queueName(consumer string) string      { return "commerce." + consumer + ".events" }
func retryQueueName(consumer string) string { return "commerce." + consumer + ".retry" }
func deadQueueName(consumer string) string  { return "commerce." + consumer + ".dlq" }

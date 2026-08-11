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
	// ConsumerXxx 是 MQ 消费方身份（与各服务注册的服务名一致）。
	// 每个消费服务一个消费者名，作为队列命名与 Inbox 幂等键的一部分。
	ConsumerOrder       = contracts.ServiceOrder
	ConsumerPayment     = contracts.ServicePayment
	ConsumerInventory   = contracts.ServiceInventory
	ConsumerFulfillment = contracts.ServiceFulfillment
)

// consumerEvents 是订阅路由表：声明每个消费者订阅哪些事件类型。
// 主队列按这里的事件列表绑定主题交换机，决定消息投递给谁；
// 变更订阅关系需要同步修改本表与对应服务的 Handler 注册。
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

// RabbitPublisher 是事件发布器：负责把事件信封可靠地投递到统一主题交换机。
// 使用单 Channel 串行发布并同步等待 publisher confirm，
// 避免多协程并发发布时 delivery tag 与确认消息错配。
type RabbitPublisher struct {
	url      string                   // RabbitMQ 连接地址（amqp://host:port）
	mu       sync.Mutex               // 串行化发布与重连，保证单 Channel 上无并发写
	conn     *amqp.Connection         // 当前连接；断开后由重连逻辑替换
	channel  *amqp.Channel            // 唯一发布信道，所有消息都经它发出
	confirms <-chan amqp.Confirmation // 发布确认通道：broker 对每条消息回 Ack/Nack
	returns  <-chan amqp.Return       // mandatory 回执通道：消息无法路由到任何队列时触发
}

// NewRabbitPublisher 只保存 URL，不建立连接。
// 连接采用懒加载：首次发布或显式 Connect 时才拨号。
func NewRabbitPublisher(url string) *RabbitPublisher { return &RabbitPublisher{url: url} }

// Connect 建立（或重建）连接、信道与拓扑，并开启发布确认。
func (p *RabbitPublisher) Connect() error {
	// 先关掉旧连接，保证 Connect 可安全地作为"重连"调用。
	p.close()
	if p.url == "" {
		return errors.New("rabbitmq url is required")
	}
	// 1. 拨号建立 TCP/AMQP 连接。
	conn, err := amqp.Dial(p.url)
	if err != nil {
		return fmt.Errorf("rabbitmq connect: %w", err)
	}
	// 2. 在连接上开一条信道：消息收发都在信道层面进行。
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("rabbitmq channel: %w", err)
	}
	// 3. 声明交换机与队列拓扑（幂等操作，重复声明无害）。
	if err := DeclareTopology(ch); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return err
	}
	// 4. 开启 publisher confirm：之后每条消息都会收到 Ack/Nack 确认。
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("rabbitmq confirm: %w", err)
	}
	// 5. 保存句柄，并订阅确认/回执通知通道（容量 1，串行消费）。
	p.conn, p.channel = conn, ch
	p.confirms = ch.NotifyPublish(make(chan amqp.Confirmation, 1))
	p.returns = ch.NotifyReturn(make(chan amqp.Return, 1))
	return nil
}

// Publish 发布一个事件：先尝试直接发布，失败则重连后重试一次。
// 加锁保证同一时刻只有一个发布动作，配合单 Channel 串行等待 confirm。
func (p *RabbitPublisher) Publish(ctx context.Context, event contracts.EventEnvelope, headers amqp.Table) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	// 首次调用或连接刚断时 publishLocked 内部会自动连接；
	// 若发布中途失败（如连接已失效），则重连后再试一次。
	if err := p.publishLocked(ctx, event, headers); err == nil {
		return nil
	}
	if err := p.Connect(); err != nil {
		return err
	}
	return p.publishLocked(ctx, event, headers)
}

// publishLocked 在已持有锁的前提下执行"序列化 → 发送 → 等待确认"完整链路。
func (p *RabbitPublisher) publishLocked(ctx context.Context, event contracts.EventEnvelope, headers amqp.Table) error {
	// 信道未就绪（未连接或已被 close）时先建立连接。
	if p.channel == nil {
		if err := p.Connect(); err != nil {
			return err
		}
	}
	// 把事件信封序列化为 JSON，作为消息体发送。
	body, err := MarshalEnvelope(event)
	if err != nil {
		return err
	}
	// 把链路追踪上下文注入消息头，消费端可据此还原调用链。
	headers = tracer.InjectAMQPHeaders(ctx, headers)
	// 发布到统一主题交换机：routing key 用事件类型，mandatory=true
	// 表示"若没有队列匹配该路由则回执返回"，便于发布方感知路由失败。
	if err := p.channel.PublishWithContext(ctx, DefaultExchange, event.EventType, true, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,  // 持久化消息：broker 重启不丢失
		MessageId:    event.EventID,    // 消息唯一 ID，消费端幂等的依据
		Type:         event.EventType,  // 消息类型（路由/过滤用）
		Timestamp:    event.OccurredAt, // 事件发生时间
		Headers:      headers,          // 附加头（链路追踪等）
		Body:         body,             // 事件信封 JSON
	}); err != nil {
		return fmt.Errorf("rabbitmq publish: %w", err)
	}
	// 同步等待 broker 确认：
	// - Ack：broker 已接收并持久化，发布成功；
	// - Nack：broker 拒绝（如内部错误）；
	// - return：mandatory 路由失败，没有任何队列匹配该事件类型；
	// - ctx 取消/超时：等待确认被打断，按失败处理（Outbox 会重试）。
	select {
	case confirmation, ok := <-p.confirms:
		if !ok {
			return errors.New("rabbitmq confirm channel closed")
		}
		if !confirmation.Ack {
			return fmt.Errorf("rabbitmq publish nack delivery_tag=%d", confirmation.DeliveryTag)
		}
		// Ack 之后仍要检查是否有路由回执：消息被 broker 丢弃也算失败。
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

// Close 关闭发布器；加锁保证与正在进行的发布互斥。
func (p *RabbitPublisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.close()
}

// close 是无锁的内部实现：清空通知通道并关闭信道与连接。
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

// DeclareTopology 声明商城统一的 RabbitMQ 拓扑：
// 三个交换机 + 每个消费者的三件套队列（主队列/重试队列/死信队列）。
// 声明是幂等的，生产者和消费者启动时都会调用，重复声明不会出错。
func DeclareTopology(ch *amqp.Channel) error {
	if ch == nil {
		return errors.New("rabbitmq channel is required")
	}
	// 主题交换机：承载全部领域事件，routing key = 事件类型，支持通配符订阅。
	if err := ch.ExchangeDeclare(DefaultExchange, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare event exchange: %w", err)
	}
	// 直连交换机：重试消息专用，按 consumer 名路由到对应重试队列。
	if err := ch.ExchangeDeclare(RetryExchange, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare retry exchange: %w", err)
	}
	// 直连交换机：死信消息专用，按 consumer 名路由到对应死信队列。
	if err := ch.ExchangeDeclare(DeadExchange, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dead exchange: %w", err)
	}
	// 为每个消费者声明其队列组，并按订阅表绑定事件路由。
	for consumer, events := range consumerEvents {
		if err := declareConsumerTopology(ch, consumer, events); err != nil {
			return err
		}
	}
	return nil
}

// declareConsumerTopology 为一个消费者声明三件套队列并完成绑定：
// - 主队列：接收订阅的领域事件；处理失败超限后按死信策略进 DLQ；
// - retry 队列：存"待重试"消息，用 TTL 到期后转投回主队列实现延迟重试；
// - dlq 队列：终态死信，供人工排查。
func declareConsumerTopology(ch *amqp.Channel, consumer string, events []string) error {
	mainQueue := queueName(consumer)
	dlq := deadQueueName(consumer)
	retry := retryQueueName(consumer)
	// 死信队列：绑定 DeadExchange，routing key 用 consumer 名。
	if _, err := ch.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare %s dlq: %w", consumer, err)
	}
	if err := ch.QueueBind(dlq, consumer, DeadExchange, false, nil); err != nil {
		return fmt.Errorf("bind %s dlq: %w", consumer, err)
	}
	// 重试队列：关键参数是 x-dead-letter-exchange="" + x-dead-letter-routing-key=主队列，
	// 即"消息在此队列过期后，转投回默认交换机的指定路由"，从而落到主队列再次消费。
	if _, err := ch.QueueDeclare(retry, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange":    "",
		"x-dead-letter-routing-key": mainQueue,
	}); err != nil {
		return fmt.Errorf("declare %s retry: %w", consumer, err)
	}
	// 重试队列绑定 RetryExchange，routing key = consumer 名。
	if err := ch.QueueBind(retry, consumer, RetryExchange, false, nil); err != nil {
		return fmt.Errorf("bind %s retry: %w", consumer, err)
	}
	// 主队列：死信策略指向 DeadExchange + consumer 路由，
	// 即被 Nack 且不重投的消息会进入该消费者的 DLQ。
	if _, err := ch.QueueDeclare(mainQueue, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange":    DeadExchange,
		"x-dead-letter-routing-key": consumer,
	}); err != nil {
		return fmt.Errorf("declare %s queue: %w", consumer, err)
	}
	// 把主队列按订阅表绑定到主题交换机：每个事件类型一条绑定。
	for _, event := range events {
		if err := ch.QueueBind(mainQueue, event, DefaultExchange, false, nil); err != nil {
			return fmt.Errorf("bind %s event=%s: %w", consumer, event, err)
		}
	}
	return nil
}

// RabbitConsumer 消费一个消费者自己的主队列，处理逻辑由调用方注入。
type RabbitConsumer struct {
	url string // RabbitMQ 连接地址
}

type publishConfirmation interface {
	WaitContext(context.Context) (bool, error)
}

type retryPublisher interface {
	PublishRetry(context.Context, string, string, amqp.Publishing) (publishConfirmation, error)
}

type rabbitRetryPublisher struct{ channel *amqp.Channel }

func (p rabbitRetryPublisher) PublishRetry(ctx context.Context, exchange, routingKey string, message amqp.Publishing) (publishConfirmation, error) {
	if p.channel == nil {
		return nil, errors.New("rabbitmq retry channel is required")
	}
	confirmation, err := p.channel.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, false, false, message)
	if err != nil {
		return nil, err
	}
	if confirmation == nil {
		return nil, errors.New("rabbitmq retry publisher confirm is not enabled")
	}
	return confirmation, nil
}

// NewRabbitConsumer 只保存 URL，连接在每次 Run 时建立。
func NewRabbitConsumer(url string) *RabbitConsumer { return &RabbitConsumer{url: url} }

// Run 建立一次消费会话并进入消费循环。
// 连接是"一次性"的：断开后本函数返回错误，由服务入口重新调用 Run 重连。
func (c *RabbitConsumer) Run(ctx context.Context, consumer string, inbox InboxStore, handlers map[string]Handler, maxAttempts int) error {
	if c == nil || c.url == "" {
		return errors.New("rabbitmq consumer url is required")
	}
	if inbox == nil {
		return errors.New("rabbitmq consumer inbox is required")
	}
	// 1. 拨号建连、开信道。
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
	// 2. 声明拓扑（幂等）：确保交换机/队列/绑定都存在。
	if err := DeclareTopology(ch); err != nil {
		return err
	}
	// 3. 开启 publisher confirm，确保 retry 消息得到 broker Ack 后才确认原消息。
	if err := ch.Confirm(false); err != nil {
		return fmt.Errorf("rabbitmq consumer confirm: %w", err)
	}
	// 4. 限流：单消费者最多 16 条未确认消息（prefetch），
	//    防止慢消费者把整个队列消息拉进内存。
	if err := ch.Qos(16, 0, false); err != nil {
		return fmt.Errorf("rabbitmq consumer qos: %w", err)
	}
	// 5. 订阅主队列；autoAck=false 表示由我们手动 Ack，保证"处理成功才确认"。
	deliveries, err := ch.Consume(queueName(consumer), "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("rabbitmq consume: %w", err)
	}
	// 6. 重试上限默认 5 次，超过后进 DLQ。
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	// 7. 主循环：ctx 取消则优雅退出；deliveries 关闭说明连接断开，返回错误由上层重连。
	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				return errors.New("rabbitmq delivery channel closed")
			}
			// 单条消息处理失败只记录日志，不影响继续消费后续消息。
			if err := c.handleDelivery(ctx, ch, consumer, inbox, handlers, maxAttempts, delivery); err != nil {
				log.Printf("message consumer delivery failed consumer=%s err=%v", consumer, err)
			}
		}
	}
}

// handleDelivery 处理单条消息：反序列化 → 过滤 → Inbox 幂等抢占 → 执行业务 → 标记终态。
// 任何一步都可能 Ack/Nack/重试，保证"至少一次投递 + 幂等消费"。
func (c *RabbitConsumer) handleDelivery(ctx context.Context, ch *amqp.Channel, consumer string, inbox InboxStore, handlers map[string]Handler, maxAttempts int, delivery amqp.Delivery) error {
	// 1. 反序列化事件信封；失败直接 Nack 且不重投（requeue=false）——载荷损坏重试无意义。
	event, err := UnmarshalEnvelope(delivery.Body)
	if err != nil {
		return delivery.Nack(false, false)
	}
	// 2. 过滤：未知事件类型（可能来自旧版本）、未来版本或未注册 Handler 直接 Ack 丢弃，
	//    不执行业务也不进 DLQ，保证新旧事件平滑演进。
	handler := lookupHandler(event, handlers)
	if handler == nil {
		return delivery.Ack(false)
	}
	// 3. Inbox 幂等抢占：以 (consumer, event_id) 插入/更新记录并获取 30s 租约。
	//    这是"防止重复处理"的第一道闸：只有抢到租约的副本才允许执行业务。
	claim, err := inbox.Claim(ctx, consumer, event, 30*time.Second)
	if err != nil {
		return c.retryOrDeadLetter(ctx, ch, consumer, event, delivery, 1, maxAttempts, err)
	}
	// 4. 已处理过：幂等命中，Ack 跳过。
	if claim.Processed {
		return delivery.Ack(false)
	}
	// 5. 未抢到租约（其他副本正在处理）：延迟重投，稍后再试。
	if !claim.Claimed {
		return c.requeueDelayed(ctx, ch, consumer, event, delivery, claim.Attempts)
	}
	// 6. 从消息头恢复链路追踪上下文，让业务处理挂在原始调用链上。
	traceCtx := tracer.ExtractAMQPHeaders(ctx, delivery.Headers)
	// 7. 执行真正的业务处理。
	if err := handler(traceCtx, event); err != nil {
		// 失败：记录失败原因（状态留在 processing，靠租约过期重试），再走重试/DLQ 流程。
		_ = inbox.MarkFailed(ctx, consumer, event.EventID, err.Error())
		return c.retryOrDeadLetter(ctx, ch, consumer, event, delivery, claim.Attempts, maxAttempts, err)
	}
	// 8. 成功：先落幂等终态（processed），再 Ack。
	//     若 MarkProcessed 失败也走重试，避免"业务已处理但幂等没记上"导致重复消费。
	if err := inbox.MarkProcessed(ctx, consumer, event.EventID); err != nil {
		return c.retryOrDeadLetter(ctx, ch, consumer, event, delivery, claim.Attempts, maxAttempts, err)
	}
	return delivery.Ack(false)
}

// lookupHandler 返回本消费者为事件注册的 Handler。
// 未知事件类型（可能来自旧版本）和高于当前支持的版本被安全忽略，未注册类型返回 nil，
// 调用方据此直接 Ack 丢弃，避免新事件把旧消费者打入 DLQ。
func lookupHandler(event contracts.EventEnvelope, handlers map[string]Handler) Handler {
	if !contracts.IsKnownEventType(event.EventType) || event.EventVersion > 1 {
		return nil
	}
	return handlers[event.EventType]
}

// retryOrDeadLetter 决定失败消息的出路：未达上限 → 进重试队列；已达上限 → Nack 不重投，
// 由主队列的死信策略自动送入 DLQ。
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

// publishRetry 将事件发布到重试交换机实现"延迟重投"，然后 Ack 原消息。
// 发布失败时 Nack 原消息（留在主队列等下一次投递），避免消息丢失。
func (c *RabbitConsumer) publishRetry(ctx context.Context, ch *amqp.Channel, consumer string, event contracts.EventEnvelope, delivery amqp.Delivery, attempt int) error {
	return c.publishRetryWith(ctx, rabbitRetryPublisher{channel: ch}, consumer, event, delivery, attempt)
}

func (c *RabbitConsumer) publishRetryWith(ctx context.Context, publisher retryPublisher, consumer string, event contracts.EventEnvelope, delivery amqp.Delivery, attempt int) error {
	// 重新序列化事件体。
	body, err := MarshalEnvelope(event)
	if err != nil {
		return delivery.Nack(false, false)
	}
	// 复制原消息头并附加尝试次数，便于排查重试历史。
	headers := amqp.Table{}
	for key, value := range delivery.Headers {
		headers[key] = value
	}
	headers["x-event-attempt"] = attempt + 1
	// 发布到 RetryExchange，routing key = consumer 名；Expiration 是 TTL 毫秒数，
	// 消息在重试队列里"活"这么久后过期，按死信配置转投回主队列实现延迟重试。
	confirmation, err := publisher.PublishRetry(ctx, RetryExchange, consumer, amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, MessageId: event.EventID, Type: event.EventType, Headers: headers, Expiration: strconv.FormatInt(RetryDelay(attempt+1, time.Second).Milliseconds(), 10), Body: body})
	if err != nil || confirmation == nil {
		return delivery.Nack(false, true)
	}
	confirmed, err := confirmation.WaitContext(ctx)
	if err != nil || !confirmed {
		return delivery.Nack(false, true)
	}
	return delivery.Ack(false)
}

// 队列命名约定：主队列按 consumer 分，重试/死信队列同名加后缀，保证各消费者互不串扰。
func queueName(consumer string) string      { return "commerce." + consumer + ".events" }
func retryQueueName(consumer string) string { return "commerce." + consumer + ".retry" }
func deadQueueName(consumer string) string  { return "commerce." + consumer + ".dlq" }

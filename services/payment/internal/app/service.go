package paymentservice

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"seckill-mall/shared/contracts"
	"seckill-mall/shared/platform/messaging"
	"strings"
	"time"
)

type Service struct {
	repository Repository
	orders     OrderClient
	secret     []byte
	now        func() time.Time
	newID      func(string) (string, error)
}

func NewService(repository Repository, orders OrderClient, secret string) (*Service, error) {
	if repository == nil || orders == nil {
		return nil, errors.New("payment repository and order client are required")
	}
	if len(strings.TrimSpace(secret)) < 32 {
		return nil, errors.New("payment secret length must be at least 32")
	}
	return &Service{repository: repository, orders: orders, secret: []byte(secret), now: time.Now, newID: randomID}, nil
}

func (s *Service) Create(ctx context.Context, userID uint64, orderID string) (Payment, string, bool, error) {
	if userID == 0 || strings.TrimSpace(orderID) == "" {
		return Payment{}, "", false, ErrInvalidRequest
	}
	order, err := s.orders.Get(ctx, userID, orderID)
	if err != nil {
		return Payment{}, "", false, err
	}
	if order.UserID != userID || order.Status != "pending_payment" {
		return Payment{}, "", false, ErrInvalidTransition
	}
	paymentNo, err := s.newID("pay")
	if err != nil {
		return Payment{}, "", false, err
	}
	payment, reused, err := s.repository.Create(ctx, Payment{PaymentNo: paymentNo, OrderID: order.OrderID, UserID: userID, AmountCents: order.TotalAmountCents, Provider: "mock", Status: StatusPending, CreatedAt: s.now().UTC(), UpdatedAt: s.now().UTC()}, s.now().UTC())
	if err != nil {
		return Payment{}, "", false, err
	}
	return payment, s.sign(payment.PaymentNo), reused, nil
}

func (s *Service) Callback(ctx context.Context, paymentNo, callbackRef, signature string) (Payment, OrderTransition, bool, error) {
	paymentNo, callbackRef = strings.TrimSpace(paymentNo), strings.TrimSpace(callbackRef)
	if paymentNo == "" || callbackRef == "" {
		return Payment{}, OrderTransition{}, false, ErrInvalidRequest
	}
	if !hmac.Equal([]byte(strings.TrimSpace(signature)), []byte(s.sign(paymentNo))) {
		return Payment{}, OrderTransition{}, false, ErrUnauthorized
	}
	payment, err := s.repository.Get(ctx, paymentNo)
	if err != nil {
		return Payment{}, OrderTransition{}, false, err
	}
	if payment.Status == StatusSucceeded {
		if payment.CallbackRef != callbackRef {
			return Payment{}, OrderTransition{}, false, ErrConflict
		}
		transition, err := s.orders.ConfirmPayment(ctx, payment.UserID, payment.OrderID, payment.PaymentNo, callbackRef)
		if err != nil {
			return Payment{}, OrderTransition{}, false, err
		}
		return payment, transition, true, nil
	}
	updated, reused, err := s.repository.MarkSucceeded(ctx, paymentNo, callbackRef, s.now().UTC())
	if err != nil {
		return Payment{}, OrderTransition{}, false, err
	}
	transition, err := s.orders.ConfirmPayment(ctx, payment.UserID, payment.OrderID, payment.PaymentNo, callbackRef)
	if err != nil {
		return Payment{}, OrderTransition{}, false, err
	}
	return updated, transition, reused, nil
}

func (s *Service) Refund(ctx context.Context, userID uint64, orderID, reason string) (Refund, OrderTransition, bool, error) {
	reason = strings.TrimSpace(reason)
	if userID == 0 || orderID == "" || reason == "" || len([]rune(reason)) > 255 {
		return Refund{}, OrderTransition{}, false, ErrInvalidRequest
	}
	order, err := s.orders.Get(ctx, userID, orderID)
	if err != nil {
		return Refund{}, OrderTransition{}, false, err
	}
	if order.UserID != userID {
		return Refund{}, OrderTransition{}, false, ErrNotFound
	}
	existing, exists, err := s.findRefundByOrder(ctx, userID, orderID)
	if err != nil {
		return Refund{}, OrderTransition{}, false, err
	}
	if exists {
		transition := OrderTransition{OrderID: orderID, Status: "refunded", Reused: true}
		return existing, transition, true, nil
	}
	payment, err := s.findSucceededPayment(ctx, orderID)
	if err != nil {
		return Refund{}, OrderTransition{}, false, err
	}
	refundNo, err := s.newID("ref")
	if err != nil {
		return Refund{}, OrderTransition{}, false, err
	}
	transition, err := s.orders.Refund(ctx, userID, orderID, refundNo, reason)
	if err != nil {
		return Refund{}, OrderTransition{}, false, err
	}
	refund, reused, err := s.repository.CreateRefund(ctx, Refund{RefundNo: refundNo, OrderID: orderID, PaymentNo: payment.PaymentNo, UserID: userID, AmountCents: payment.AmountCents, Reason: reason, Status: RefundSucceeded, CreatedAt: s.now().UTC(), CompletedAt: s.now().UTC()}, s.now().UTC())
	if err != nil {
		return Refund{}, OrderTransition{}, false, err
	}
	return refund, transition, reused, nil
}

type refundFinder interface {
	FindRefundByOrder(ctx context.Context, userID uint64, orderID string) (Refund, bool, error)
}

func (s *Service) findSucceededPayment(ctx context.Context, orderID string) (Payment, error) {
	finder, ok := s.repository.(interface {
		FindSucceededPayment(context.Context, string) (Payment, error)
	})
	if !ok {
		return Payment{}, ErrInvalidTransition
	}
	return finder.FindSucceededPayment(ctx, orderID)
}

func (s *Service) findRefundByOrder(ctx context.Context, userID uint64, orderID string) (Refund, bool, error) {
	finder, ok := s.repository.(refundFinder)
	if !ok {
		return Refund{}, false, ErrInvalidTransition
	}
	return finder.FindRefundByOrder(ctx, userID, orderID)
}

func (s *Service) sign(paymentNo string) string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(paymentNo))
	return hex.EncodeToString(mac.Sum(nil))
}

func randomID(prefix string) (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(buffer)), nil
}

func paymentSucceededEvent(value Payment, callbackRef string, now time.Time) (contracts.EventEnvelope, error) {
	return messaging.NewEvent(fmt.Sprintf("payment.succeeded:%s", value.PaymentNo), contracts.EventPaymentSucceeded, "order", value.OrderID, contracts.PaymentSucceededPayload{PaymentNo: value.PaymentNo, OrderID: value.OrderID, UserID: value.UserID, AmountCents: value.AmountCents, CallbackRef: callbackRef}, now)
}

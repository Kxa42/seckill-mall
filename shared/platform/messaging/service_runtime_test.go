package messaging

import (
	"context"
	"testing"
)

type runtimeSinkOwner struct{ sink EventSink }

func (o *runtimeSinkOwner) SetEventSink(sink EventSink) { o.sink = sink }

func TestMemoryServiceRuntimeWiresStoreWithoutMQ(t *testing.T) {
	owner := &runtimeSinkOwner{}
	runtime, err := NewMemoryServiceRuntime(owner, "")
	if err != nil {
		t.Fatal(err)
	}
	if owner.sink == nil {
		t.Fatal("memory runtime did not wire the event sink")
	}
	if runtime.Enabled() {
		t.Fatal("runtime must stay disabled when RabbitMQ URL is empty")
	}
	runtime.Start(context.Background(), "test-consumer", nil)
	if err := runtime.Close(); err != nil {
		t.Fatalf("disabled runtime close: %v", err)
	}
}

func TestServiceRuntimeValidatesDependencies(t *testing.T) {
	if _, err := NewMemoryServiceRuntime(nil, ""); err == nil {
		t.Fatal("nil event sink owner should be rejected")
	}
	if _, err := NewSQLServiceRuntime(&runtimeSinkOwner{}, nil, "outbox", "inbox", ""); err == nil {
		t.Fatal("nil SQL database should be rejected")
	}
}

func TestServiceRuntimeEnablesConfiguredPublisherLazily(t *testing.T) {
	runtime, err := NewMemoryServiceRuntime(&runtimeSinkOwner{}, " amqp://guest:guest@localhost/ ")
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.Enabled() {
		t.Fatal("runtime should enable RabbitMQ when URL is configured")
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("lazy publisher close: %v", err)
	}
}

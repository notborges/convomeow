package app

import (
	"sync"
	"testing"
)

func TestNotificationsDisconnectSlowConsumer(t *testing.T) {
	s := &Service{}
	slow, cancelSlow := s.Subscribe()
	defer cancelSlow()
	fast, cancelFast := s.Subscribe()
	defer cancelFast()
	change := Notification{Type: AccountsChanged, AccountID: "account"}
	for i := 0; i < 65; i++ {
		s.publish(change)
		if got := <-fast; got != change {
			t.Fatal("wrong notification")
		}
	}
	count := 0
	for range slow {
		count++
	}
	if count != 64 {
		t.Fatalf("buffered %d changes", count)
	}
	s.publish(change)
	if <-fast != change {
		t.Fatal("slow consumer blocked healthy subscriber")
	}
}

func TestNotificationCleanupIsConcurrentAndIdempotent(t *testing.T) {
	s := &Service{}
	ch, cancel := s.Subscribe()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.publish(Notification{Type: AccountsChanged}); cancel() }()
	}
	wg.Wait()
	for range ch {
	}
	s.closeNotifications()
	closed, cancelClosed := s.Subscribe()
	cancelClosed()
	if _, open := <-closed; open {
		t.Fatal("subscribed after shutdown")
	}
	if len(s.notifications.subscribers) != 0 {
		t.Fatal("subscriber leak")
	}
}

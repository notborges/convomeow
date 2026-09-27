package app

import (
	"context"
	"testing"
	"time"

	"github.com/notborges/convomeow/internal/core"
)

type activitySession struct {
	core.Session
	calls []core.ChatActivity
}

func (s *activitySession) SetOnline(context.Context, bool) error { return nil }
func (s *activitySession) SendChatPresence(_ context.Context, _ string, a core.ChatActivity) error {
	s.calls = append(s.calls, a)
	return nil
}

func TestActivityLeasesThrottleAggregateAndExpire(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session := &activitySession{}
	rt := &runtimeAccount{state: "connected", session: session}
	service := &Service{ctx: ctx}
	defer service.clearActivity(rt)
	update := func(client string, state core.ChatActivity) {
		t.Helper()
		if err := service.updateActivity(ctx, rt, "chat", client, state); err != nil {
			t.Fatal(err)
		}
	}
	update("one", core.ActivityTyping)
	update("one", core.ActivityTyping)
	update("two", core.ActivityTyping)
	update("one", core.ActivityPaused)
	if len(session.calls) != 1 || session.calls[0] != core.ActivityTyping {
		t.Fatalf("one tab stopped another: %v", session.calls)
	}
	update("two", core.ActivityRecording)
	if len(session.calls) != 2 || session.calls[1] != core.ActivityRecording {
		t.Fatal("recording transition lost")
	}
	rt.activity.mu.Lock()
	chat := rt.activity.chats["chat"]
	chat.leases["two"] = activityLease{core.ActivityRecording, time.Now().Add(-time.Second)}
	err := service.flushActivityLocked(ctx, rt, "chat", chat)
	rt.activity.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if len(session.calls) != 3 || session.calls[2] != core.ActivityPaused || len(rt.activity.chats) != 0 {
		t.Fatal("expired composer not paused and removed")
	}
	update("one", core.ActivityTyping)
	service.clearActivity(rt)
	if len(rt.activity.chats) != 0 {
		t.Fatal("disconnect kept activity")
	}
}

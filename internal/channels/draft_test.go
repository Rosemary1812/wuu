package channels

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestHeldDraftResolvePaths(t *testing.T) {
	ctx := context.Background()
	t.Run("as_is commits against fresh basis", func(t *testing.T) {
		service := openTestService(t, nil)
		alpha := createTestAgent(t, service, "Alpha")
		beta := createTestAgent(t, service, "Beta")
		room := createTestRoom(t, service, alpha, beta)
		first, err := service.SendAgent(ctx, AgentSendParams{RoomID: room.ID, AgentID: alpha.Agent.ID, Token: alpha.Token, Body: "one", BasisSeq: 0})
		if err != nil {
			t.Fatal(err)
		}
		held, err := service.SendAgent(ctx, AgentSendParams{RoomID: room.ID, AgentID: beta.Agent.ID, Token: beta.Token, Body: "independent", BasisSeq: 0})
		if err != nil || held.Status != SendHeld || held.Draft == nil {
			t.Fatalf("held send = %#v, err = %v", held, err)
		}
		basis := first.Message.Seq
		resolved, err := service.ResolveDraft(ctx, ResolveDraftParams{
			AgentID: beta.Agent.ID, Token: beta.Token, DraftID: held.Draft.ID, Resolution: DraftAsIs, BasisSeq: &basis,
		})
		if err != nil {
			t.Fatal(err)
		}
		if resolved.Status != SendCommitted || resolved.Message == nil || resolved.Message.Seq != 2 || resolved.Message.Body != "independent" {
			t.Fatalf("resolved draft = %#v", resolved)
		}
	})

	t.Run("silent drops without posting", func(t *testing.T) {
		service := openTestService(t, nil)
		alpha := createTestAgent(t, service, "Alpha")
		beta := createTestAgent(t, service, "Beta")
		room := createTestRoom(t, service, alpha, beta)
		if _, err := service.SendAgent(ctx, AgentSendParams{RoomID: room.ID, AgentID: alpha.Agent.ID, Token: alpha.Token, Body: "one", BasisSeq: 0}); err != nil {
			t.Fatal(err)
		}
		held, err := service.SendAgent(ctx, AgentSendParams{RoomID: room.ID, AgentID: beta.Agent.ID, Token: beta.Token, Body: "duplicate", BasisSeq: 0})
		if err != nil {
			t.Fatal(err)
		}
		resolved, err := service.ResolveDraft(ctx, ResolveDraftParams{
			AgentID: beta.Agent.ID, Token: beta.Token, DraftID: held.Draft.ID, Resolution: DraftSilent,
		})
		if err != nil || resolved.Draft.State != DraftDropped {
			t.Fatalf("silent resolution = %#v, err = %v", resolved, err)
		}
		messages, _ := service.ListMessages(ctx, room.ID, 0, 10)
		if len(messages) != 1 {
			t.Fatalf("silent resolution posted messages: %#v", messages)
		}
	})

	t.Run("anyway requires two holds", func(t *testing.T) {
		service := openTestService(t, nil)
		alpha := createTestAgent(t, service, "Alpha")
		beta := createTestAgent(t, service, "Beta")
		room := createTestRoom(t, service, alpha, beta)
		first, _ := service.SendAgent(ctx, AgentSendParams{RoomID: room.ID, AgentID: alpha.Agent.ID, Token: alpha.Token, Body: "one", BasisSeq: 0})
		held, _ := service.SendAgent(ctx, AgentSendParams{RoomID: room.ID, AgentID: beta.Agent.ID, Token: beta.Token, Body: "still important", BasisSeq: 0})
		if _, err := service.ResolveDraft(ctx, ResolveDraftParams{
			AgentID: beta.Agent.ID, Token: beta.Token, DraftID: held.Draft.ID, Resolution: DraftAnyway,
		}); err == nil {
			t.Fatal("anyway committed a draft held only once")
		}
		second, err := service.SendHuman(ctx, HumanSendParams{RoomID: room.ID, HumanID: "human-1", Body: "more context"})
		if err != nil {
			t.Fatal(err)
		}
		staleBasis := first.Message.Seq
		reheld, err := service.ResolveDraft(ctx, ResolveDraftParams{
			AgentID: beta.Agent.ID, Token: beta.Token, DraftID: held.Draft.ID, Resolution: DraftAsIs, BasisSeq: &staleBasis,
		})
		if err != nil || reheld.Status != SendHeld || reheld.Draft.HoldCount != 2 || reheld.Delta.Count != 1 {
			t.Fatalf("second hold = %#v, err = %v (latest seq %d)", reheld, err, second.Message.Seq)
		}
		forced, err := service.ResolveDraft(ctx, ResolveDraftParams{
			AgentID: beta.Agent.ID, Token: beta.Token, DraftID: held.Draft.ID, Resolution: DraftAnyway,
		})
		if err != nil || forced.Status != SendCommitted || forced.Message == nil || forced.Message.Seq != 3 {
			t.Fatalf("forced resolution = %#v, err = %v", forced, err)
		}
	})
}

func TestRevisedSendDropsHeldDraft(t *testing.T) {
	ctx := context.Background()
	service := openTestService(t, nil)
	alpha := createTestAgent(t, service, "Alpha")
	beta := createTestAgent(t, service, "Beta")
	room := createTestRoom(t, service, alpha, beta)
	first, _ := service.SendAgent(ctx, AgentSendParams{RoomID: room.ID, AgentID: alpha.Agent.ID, Token: alpha.Token, Body: "covered", BasisSeq: 0})
	held, _ := service.SendAgent(ctx, AgentSendParams{RoomID: room.ID, AgentID: beta.Agent.ID, Token: beta.Token, Body: "old draft", BasisSeq: 0})
	revised, err := service.SendAgent(ctx, AgentSendParams{RoomID: room.ID, AgentID: beta.Agent.ID, Token: beta.Token, Body: "new incremental point", BasisSeq: first.Message.Seq})
	if err != nil || revised.Status != SendCommitted {
		t.Fatalf("revised send = %#v, err = %v", revised, err)
	}
	drafts, err := service.ListDrafts(ctx, beta.Agent.ID, beta.Token)
	if err != nil || len(drafts) != 0 {
		t.Fatalf("held drafts after revise = %#v, err = %v; old = %s", drafts, err, held.Draft.ID)
	}
}

func TestDraftBasisUsesTargetScope(t *testing.T) {
	ctx := context.Background()
	service := openTestService(t, nil)
	alpha := createTestAgent(t, service, "Alpha")
	room := createTestRoom(t, service, alpha)
	root, _ := service.SendHuman(ctx, HumanSendParams{RoomID: room.ID, HumanID: "human-1", Body: "topic"})
	if _, err := service.SendHuman(ctx, HumanSendParams{RoomID: room.ID, HumanID: "human-1", ReplyTo: root.Message.ID, Body: "thread detail"}); err != nil {
		t.Fatal(err)
	}
	main, err := service.SendAgent(ctx, AgentSendParams{
		RoomID: room.ID, AgentID: alpha.Agent.ID, Token: alpha.Token, Body: "new main point", BasisSeq: root.Message.Seq,
	})
	if err != nil || main.Status != SendCommitted || main.Message.Seq != 3 {
		t.Fatalf("thread traffic incorrectly invalidated main scope: %#v, err = %v", main, err)
	}
	thread, err := service.SendAgent(ctx, AgentSendParams{
		RoomID: room.ID, AgentID: alpha.Agent.ID, Token: alpha.Token,
		ThreadID: root.Message.ID, Body: "thread answer", BasisSeq: 2,
	})
	if err != nil || thread.Status != SendCommitted || thread.Message.Seq != 4 {
		t.Fatalf("thread send = %#v, err = %v", thread, err)
	}
	check, err := service.Check(ctx, alpha.Agent.ID, alpha.Token)
	if err != nil {
		t.Fatal(err)
	}
	if got := scopeSeq(check.Scopes, room.ID, ""); got != main.Message.Seq {
		t.Fatalf("main scope seq = %d, want %d", got, main.Message.Seq)
	}
	if got := scopeSeq(check.Scopes, room.ID, root.Message.ID); got != thread.Message.Seq {
		t.Fatalf("thread scope seq = %d, want %d", got, thread.Message.Seq)
	}
}

func TestHeldDraftExpiresAfterTwentyFourHours(t *testing.T) {
	ctx := context.Background()
	service := openTestService(t, nil)
	alpha := createTestAgent(t, service, "Alpha")
	beta := createTestAgent(t, service, "Beta")
	room := createTestRoom(t, service, alpha, beta)
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	if _, err := service.SendAgent(ctx, AgentSendParams{RoomID: room.ID, AgentID: alpha.Agent.ID, Token: alpha.Token, Body: "one", BasisSeq: 0}); err != nil {
		t.Fatal(err)
	}
	held, err := service.SendAgent(ctx, AgentSendParams{RoomID: room.ID, AgentID: beta.Agent.ID, Token: beta.Token, Body: "late", BasisSeq: 0})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now.Add(DraftExpiry + time.Second) }
	if _, err := service.ResolveDraft(ctx, ResolveDraftParams{
		AgentID: beta.Agent.ID, Token: beta.Token, DraftID: held.Draft.ID, Resolution: DraftSilent,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("resolving expired draft error = %v, want ErrConflict", err)
	}
}

func TestExpireDraftsMarksAbandonedHeldDraftExpired(t *testing.T) {
	ctx := context.Background()
	service := openTestService(t, nil)
	alpha := createTestAgent(t, service, "Alpha")
	beta := createTestAgent(t, service, "Beta")
	room := createTestRoom(t, service, alpha, beta)
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	if _, err := service.SendAgent(ctx, AgentSendParams{RoomID: room.ID, AgentID: alpha.Agent.ID, Token: alpha.Token, Body: "one", BasisSeq: 0}); err != nil {
		t.Fatal(err)
	}
	held, err := service.SendAgent(ctx, AgentSendParams{RoomID: room.ID, AgentID: beta.Agent.ID, Token: beta.Token, Body: "late", BasisSeq: 0})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now.Add(DraftExpiry + time.Second) }
	if err := service.ExpireDrafts(ctx); err != nil {
		t.Fatal(err)
	}
	var state DraftState
	if err := service.db.QueryRowContext(ctx, `SELECT state FROM drafts WHERE id = ?`, held.Draft.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != DraftExpired {
		t.Fatalf("draft state = %q, want %q", state, DraftExpired)
	}
}

func TestM2CountingGameHasNoDuplicatePosts(t *testing.T) {
	ctx := context.Background()
	service := openTestService(t, nil)
	alpha := createTestAgent(t, service, "Alpha")
	beta := createTestAgent(t, service, "Beta")
	room := createTestRoom(t, service, alpha, beta)
	start, err := service.SendHuman(ctx, HumanSendParams{RoomID: room.ID, HumanID: "human-1", Body: "count to 20"})
	if err != nil {
		t.Fatal(err)
	}
	current := start.Message.Seq
	agents := []AgentCredential{alpha, beta}
	for number := 1; number <= 20; number++ {
		winner := agents[number%2]
		collision := agents[(number+1)%2]
		body := fmt.Sprintf("%d", number)
		posted, err := service.SendAgent(ctx, AgentSendParams{
			RoomID: room.ID, AgentID: winner.Agent.ID, Token: winner.Token, Body: body, BasisSeq: current,
		})
		if err != nil || posted.Status != SendCommitted {
			t.Fatalf("count %d post = %#v, err = %v", number, posted, err)
		}
		held, err := service.SendAgent(ctx, AgentSendParams{
			RoomID: room.ID, AgentID: collision.Agent.ID, Token: collision.Token, Body: body, BasisSeq: current,
		})
		if err != nil || held.Status != SendHeld {
			t.Fatalf("count %d collision = %#v, err = %v", number, held, err)
		}
		if _, err := service.ResolveDraft(ctx, ResolveDraftParams{
			AgentID: collision.Agent.ID, Token: collision.Token, DraftID: held.Draft.ID, Resolution: DraftSilent,
		}); err != nil {
			t.Fatalf("count %d drop collision: %v", number, err)
		}
		current = posted.Message.Seq
	}
	messages, err := service.ListMessages(ctx, room.ID, start.Message.Seq, 100)
	if err != nil || len(messages) != 20 {
		t.Fatalf("counting messages = %d, err = %v", len(messages), err)
	}
	for index, message := range messages {
		if want := fmt.Sprintf("%d", index+1); message.Body != want {
			t.Fatalf("counting message %d = %q, want %q", index, message.Body, want)
		}
	}
}

func TestM2ParallelOpinionsCanCommitAsIs(t *testing.T) {
	ctx := context.Background()
	service := openTestService(t, nil)
	alpha := createTestAgent(t, service, "Alpha")
	beta := createTestAgent(t, service, "Beta")
	gamma := createTestAgent(t, service, "Gamma")
	room := createTestRoom(t, service, alpha, beta, gamma)
	prompt, err := service.SendHuman(ctx, HumanSendParams{
		RoomID: room.ID, HumanID: "human-1", Body: "@Alpha @Beta @Gamma give independent views",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, credential := range []AgentCredential{alpha, beta, gamma} {
		check, err := service.Check(ctx, credential.Agent.ID, credential.Token)
		if err != nil || scopeSeq(check.Scopes, room.ID, "") != prompt.Message.Seq {
			t.Fatalf("%s initial check = %#v, err = %v", credential.Agent.Name, check, err)
		}
	}
	first, err := service.SendAgent(ctx, AgentSendParams{RoomID: room.ID, AgentID: alpha.Agent.ID, Token: alpha.Token, Body: "backend view", BasisSeq: prompt.Message.Seq})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := service.SendAgent(ctx, AgentSendParams{RoomID: room.ID, AgentID: beta.Agent.ID, Token: beta.Token, Body: "operations view", BasisSeq: prompt.Message.Seq})
	third, _ := service.SendAgent(ctx, AgentSendParams{RoomID: room.ID, AgentID: gamma.Agent.ID, Token: gamma.Token, Body: "product view", BasisSeq: prompt.Message.Seq})
	if second.Status != SendHeld || third.Status != SendHeld || third.Delta.Count != 1 {
		t.Fatalf("parallel opinion holds: second=%#v third=%#v", second, third)
	}
	basis := first.Message.Seq
	secondResolved, err := service.ResolveDraft(ctx, ResolveDraftParams{
		AgentID: beta.Agent.ID, Token: beta.Token, DraftID: second.Draft.ID, Resolution: DraftAsIs, BasisSeq: &basis,
	})
	if err != nil {
		t.Fatal(err)
	}
	basis = secondResolved.Message.Seq
	thirdResolved, err := service.ResolveDraft(ctx, ResolveDraftParams{
		AgentID: gamma.Agent.ID, Token: gamma.Token, DraftID: third.Draft.ID, Resolution: DraftAsIs, BasisSeq: &basis,
	})
	if err != nil || thirdResolved.Status != SendCommitted {
		t.Fatalf("third as-is = %#v, err = %v", thirdResolved, err)
	}
	messages, _ := service.ListMessages(ctx, room.ID, prompt.Message.Seq, 10)
	if len(messages) != 3 || messages[0].Body != "backend view" || messages[1].Body != "operations view" || messages[2].Body != "product view" {
		t.Fatalf("independent opinions = %#v", messages)
	}
}

func scopeSeq(scopes []ScopeSequence, roomID, threadID string) int64 {
	for _, scope := range scopes {
		if scope.RoomID == roomID && scope.ThreadID == threadID {
			return scope.Seq
		}
	}
	return -1
}

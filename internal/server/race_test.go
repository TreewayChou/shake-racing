package server_test

import (
	"fmt"
	"testing"
	"time"

	"racing/internal/servertest"
)

func TestRaceConfigReachesPhonesAndCountdownCarriesTheTarget(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	phone := h.Phone()
	phone.Join("小明")
	phone.ExpectConfig()

	host.SetConfig(map[string]any{"mode": "individual-race"})
	if got, want := phone.ExpectConfig(), (servertest.Config{Mode: "individual-race", Target: 200, TimeCapMs: 90000}); got != want {
		t.Errorf("default race config = %+v, want %+v", got, want)
	}
	host.SetConfig(map[string]any{"target": 300, "timeCapMs": 60000})
	if got, want := phone.ExpectConfig(), (servertest.Config{Mode: "individual-race", Target: 300, TimeCapMs: 60000}); got != want {
		t.Errorf("race config = %+v, want %+v", got, want)
	}
	if s := host.NextSnapshot(h); s.Mode != "individual-race" || s.Target != 300 || s.TimeCapMs != 60000 {
		t.Errorf("snapshot mode/target/cap = %q/%d/%d, want individual-race/300/60000", s.Mode, s.Target, s.TimeCapMs)
	}

	host.Start()
	if cd := phone.ExpectCountdown(); cd.Target != 300 || cd.TimeCapMs != 60000 || cd.DurationMs != 0 {
		t.Errorf("countdown = %+v, want target 300 cap 60000 and no duration", cd)
	}
}

// startIndividualRace joins one phone per name, starts an individual race
// to target with the given time cap, and advances to the synced start.
func startIndividualRace(t *testing.T, target, timeCapMs int, names ...string) *race {
	t.Helper()
	r := &race{h: servertest.Start(t)}
	r.host = r.h.Host()
	for _, n := range names {
		p := r.h.Phone()
		r.ids = append(r.ids, p.Join(n).PlayerID)
		r.phones = append(r.phones, p)
	}
	r.host.SetConfig(map[string]any{"mode": "individual-race", "target": target, "timeCapMs": timeCapMs})
	r.host.Start()
	r.startAt = r.phones[0].ExpectCountdown().StartAt
	r.advanceTo(0)
	return r
}

func TestRaceEndsAtTheTimeCap(t *testing.T) {
	r := startIndividualRace(t, 100, 60000, "甲", "乙")

	r.advanceTo(20000)
	r.phones[0].Finish(100, 19800)
	r.phones[1].Progress(50, 20000)
	r.phones[0].Echo()
	r.phones[1].Echo()

	r.advanceTo(60000 - 100)
	if s := r.host.NextSnapshot(r.h); s.Phase != "grace" || s.EndAt != r.startAt+60000 {
		t.Errorf("at the cap: phase %q endAt %d, want grace at %d", s.Phase, s.EndAt, r.startAt+60000)
	}
	r.phones[1].ExpectEnd()
	r.advanceTo(60000 + graceMs)

	if res := r.phones[0].ExpectResult(); res.Rank != 1 || !res.Finished || res.ElapsedMs != 19800 || res.TotalShakes != 100 {
		t.Errorf("finisher result = %+v, want rank 1 finished in 19800 ms with 100", res)
	}
	if res := r.phones[1].ExpectResult(); res.Rank != 2 || res.Finished || res.TotalShakes != 50 {
		t.Errorf("non-finisher result = %+v, want rank 2 unfinished with 50", res)
	}
}

func TestFinishersRankByPhoneTimeThenOthersByTotal(t *testing.T) {
	r := startIndividualRace(t, 100, 60000, "甲", "乙", "丙", "丁", "戊")

	r.advanceTo(21000)
	r.phones[1].Finish(100, 21000) // arrives first, but finished later
	r.phones[1].Echo()
	r.advanceTo(21500)
	r.phones[0].Finish(100, 20000) // finished first, delayed by the network
	r.phones[3].Finish(100, 21000) // same time as 乙, joined later
	r.phones[2].Progress(70, 21500)
	r.phones[4].Progress(90, 21500)
	for _, p := range r.phones {
		p.Echo()
	}

	s := r.host.NextSnapshot(r.h)
	for _, p := range s.Players {
		if p.Name == "甲" && (!p.Finished || p.ElapsedMs != 20000 || p.Total != 100) {
			t.Errorf("甲 in snapshot = %+v, want finished in 20000 ms with 100", p)
		}
		if p.Name == "丙" && (p.Finished || p.ElapsedMs != 0) {
			t.Errorf("丙 in snapshot = %+v, want unfinished", p)
		}
	}
	if top := s.Top[0]; !top.Finished || top.ElapsedMs != 20000 {
		t.Errorf("top[0] = %+v, want finished in 20000 ms", top)
	}
	wantTop := []string{"甲", "乙", "丁", "戊", "丙"}
	for i, want := range wantTop {
		if s.Top[i].Name != want {
			t.Errorf("live top[%d] = %s, want %s", i, s.Top[i].Name, want)
		}
	}
	r.advanceTo(60000 + graceMs)
	for i, want := range []struct {
		rank     int
		finished bool
		ms       int64
	}{{1, true, 20000}, {2, true, 21000}, {5, false, 0}, {3, true, 21000}, {4, false, 0}} {
		res := r.phones[i].ExpectResult()
		if res.Rank != want.rank || res.Finished != want.finished || res.ElapsedMs != want.ms {
			t.Errorf("%s result = %+v, want rank %d finished %v in %d ms", wantTop[want.rank-1], res, want.rank, want.finished, want.ms)
		}
	}
}

func TestRaceEndsWhenTheTop15HaveFinished(t *testing.T) {
	names := make([]string, 17)
	for i := range names {
		names[i] = fmt.Sprintf("p%d", i)
	}
	r := startIndividualRace(t, 100, 90000, names...)

	r.advanceTo(30000)
	for i := range 14 {
		r.phones[i].Finish(100, int64(20000+i*100))
		r.phones[i].Echo()
	}
	if s := r.host.NextSnapshot(r.h); s.Phase != "racing" {
		t.Fatalf("with 14 finished: phase %q, want racing", s.Phase)
	}
	finalAt := r.h.NowMs()
	r.phones[14].Finish(100, 29000)
	r.phones[14].Echo()
	r.phones[16].ExpectEnd()

	s := r.host.NextSnapshot(r.h)
	if s.Phase != "grace" || s.EndAt != finalAt {
		t.Errorf("after the 15th finish: phase %q endAt %d, want grace at %d", s.Phase, s.EndAt, finalAt)
	}
	// A 16th finish delivered during grace still counts.
	r.phones[15].Finish(100, 29900)
	r.phones[15].Echo()
	r.h.Advance(graceMs * time.Millisecond)
	if res := r.phones[15].ExpectResult(); res.Rank != 16 || !res.Finished {
		t.Errorf("16th finisher result = %+v, want rank 16 finished", res)
	}
	if res := r.phones[16].ExpectResult(); res.Rank != 17 || res.Finished {
		t.Errorf("last result = %+v, want rank 17 unfinished", res)
	}
}

func TestSmallRaceEndsWhenEveryoneHasFinished(t *testing.T) {
	r := startIndividualRace(t, 100, 90000, "甲", "乙")

	r.advanceTo(12000)
	r.phones[0].Finish(100, 11000)
	r.phones[0].Echo()
	if s := r.host.NextSnapshot(r.h); s.Phase != "racing" {
		t.Fatalf("with 1 of 2 finished: phase %q, want racing", s.Phase)
	}
	r.phones[1].Finish(100, 12000)
	r.phones[1].Echo()

	r.phones[0].ExpectEnd()
	if s := r.host.NextSnapshot(r.h); s.Phase != "grace" {
		t.Errorf("with everyone finished: phase %q, want grace", s.Phase)
	}
}

func TestHostEndsTheRaceEarly(t *testing.T) {
	r := startIndividualRace(t, 200, 90000, "甲", "乙")

	r.advanceTo(30000)
	r.phones[0].Progress(120, 30000)
	r.phones[1].Progress(150, 30000)
	r.phones[0].Echo()
	r.phones[1].Echo()
	endedAt := r.h.NowMs()
	r.host.End()

	r.phones[0].ExpectEnd()
	if s := r.host.NextSnapshot(r.h); s.Phase != "grace" || s.EndAt != endedAt {
		t.Errorf("after 結束: phase %q endAt %d, want grace at %d", s.Phase, s.EndAt, endedAt)
	}
	r.phones[0].Progress(125, 30100) // the final report, delayed by the network
	r.phones[0].Echo()
	r.h.Advance(graceMs * time.Millisecond)
	if res := r.phones[0].ExpectResult(); res.Rank != 2 || res.TotalShakes != 125 {
		t.Errorf("甲 result = %+v, want rank 2 with 125", res)
	}
	if res := r.phones[1].ExpectResult(); res.Rank != 1 {
		t.Errorf("乙 result = %+v, want rank 1", res)
	}
}

func TestEndOutsideRacingIsIgnored(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	h.Phone().Join("小明")
	host.End() // lobby
	host.Start()
	host.End() // countdown

	if s := host.NextSnapshot(h); s.Phase != "countdown" {
		t.Errorf("phase = %q, want countdown", s.Phase)
	}
}

func TestHostEndsATimedRoundEarly(t *testing.T) {
	r := startRace(t, 60000, "小明")

	r.advanceTo(10000)
	r.host.End()

	r.phones[0].ExpectEnd()
	if s := r.host.NextSnapshot(r.h); s.Phase != "grace" || s.EndAt != r.startAt+10000 {
		t.Errorf("after 結束: phase %q endAt %d, want grace at %d", s.Phase, s.EndAt, r.startAt+10000)
	}
}

func TestFinishFasterThanTheShakeRateIsNotAFinish(t *testing.T) {
	r := startIndividualRace(t, 100, 60000, "小明")
	p := r.phones[0]

	r.advanceTo(5000)
	p.Finish(100, 5000) // 12.5/s allows about 65 in 5 s
	p.Echo()
	r.advanceTo(60000 + graceMs)

	if res := p.ExpectResult(); res.Finished || res.TotalShakes >= 100 {
		t.Errorf("result = %+v, want an unfinished total under 100", res)
	}
}

func TestOnlyOfferedTargetsAndCapsAreAccepted(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	host.SetConfig(map[string]any{"mode": "individual-race"})

	for _, n := range []int{100, 500, 250, 0, -100} {
		host.SetConfig(map[string]any{"target": n})
	}
	for _, ms := range []int{120000, 45000, 0} {
		host.SetConfig(map[string]any{"timeCapMs": ms})
	}
	host.SetConfig(map[string]any{"mode": "team-anything"})

	if s := host.NextSnapshot(h); s.Mode != "individual-race" || s.Target != 500 || s.TimeCapMs != 120000 {
		t.Errorf("snapshot mode/target/cap = %q/%d/%d, want individual-race/500/120000", s.Mode, s.Target, s.TimeCapMs)
	}
}

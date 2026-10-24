package server_test

import (
	"fmt"
	"testing"
	"time"

	"racing/internal/servertest"
)

// race is a timed round under way.
type race struct {
	h       *servertest.Harness
	host    *servertest.Host
	phones  []*servertest.Phone
	ids     []string
	startAt int64 // server clock (phones here have no clock offset)
}

// startRace joins one phone per name, starts a timed round of durationMs
// and advances the clock to the synced start.
func startRace(t *testing.T, durationMs int, names ...string) *race {
	t.Helper()
	r := &race{h: servertest.Start(t)}
	r.host = r.h.Host()
	for _, n := range names {
		p := r.h.Phone()
		r.ids = append(r.ids, p.Join(n).PlayerID)
		r.phones = append(r.phones, p)
	}
	r.host.SetConfig(map[string]any{"durationMs": durationMs})
	r.host.Start()
	r.startAt = r.phones[0].ExpectCountdown().StartAt
	r.advanceTo(0)
	return r
}

// advanceTo moves the clock to elapsedMs after the synced start.
func (r *race) advanceTo(elapsedMs int64) {
	r.h.Advance(time.Duration(r.startAt+elapsedMs-r.h.NowMs()) * time.Millisecond)
}

// total is player i's accepted total in a fresh snapshot.
func (r *race) total(t *testing.T, i int) int {
	t.Helper()
	for _, p := range r.host.NextSnapshot(r.h).Players {
		if p.ID == r.ids[i] {
			return p.Total
		}
	}
	t.Fatalf("player %d missing from snapshot", i)
	return 0
}

func TestDroppedPacketLosesNoShakes(t *testing.T) {
	r := startRace(t, 30000, "小明")
	p := r.phones[0]

	r.advanceTo(1000)
	p.Progress(10, 1000)
	r.advanceTo(1150) // the 12-shake report at 1150 ms is lost
	r.advanceTo(1300)
	p.Progress(15, 1300)
	p.Echo()

	if got := r.total(t, 0); got != 15 {
		t.Errorf("total = %d, want 15", got)
	}
	r.advanceTo(30000 + graceMs)
	if res := p.ExpectResult(); res.TotalShakes != 15 {
		t.Errorf("result total = %d, want 15", res.TotalShakes)
	}
}

func TestTotalsNeverDecrease(t *testing.T) {
	r := startRace(t, 30000, "小明")
	p := r.phones[0]

	r.advanceTo(2150)
	p.Progress(20, 2000)
	p.Progress(12, 2150)
	p.Echo()

	if got := r.total(t, 0); got != 20 {
		t.Errorf("total = %d, want 20", got)
	}
}

func TestTotalsAreCappedAtTheShakeRate(t *testing.T) {
	r := startRace(t, 30000, "小明")
	p := r.phones[0]

	r.advanceTo(2000)
	p.Progress(100, 2000) // 12.5/s allows 25 in 2 s
	p.Echo()

	if got := r.total(t, 0); got < 25 || got > 28 {
		t.Errorf("total = %d, want 25 plus a small slack", got)
	}
}

func TestImplausibleElapsedIsIgnored(t *testing.T) {
	r := startRace(t, 30000, "小明")
	p := r.phones[0]

	r.advanceTo(2000)
	p.Progress(20, 9000) // claims 9 s into a race the server has seen 2 s of
	p.Progress(5, -100)
	p.Echo()

	if got := r.total(t, 0); got != 0 {
		t.Errorf("total = %d, want 0", got)
	}
}

func TestFinishClaimedFarBehindTheServerIsClampedToTheStallTolerance(t *testing.T) {
	r := startIndividualRace(t, 100, 60000, "小明")
	p := r.phones[0]

	// At 30 s a finish timed 8 s arrives: the rate cap allows 100 shakes
	// in 8 s, but no network stall explains a report 22 s old.
	r.advanceTo(30000)
	p.Finish(100, 8000)
	p.Echo()

	got, _ := r.host.NextSnapshot(r.h).Player(r.ids[0])
	if want := int64(30000 - 10000); !got.Finished || got.ElapsedMs != want {
		t.Errorf("finish = %+v, want finished at %d ms (observed minus the 10 s stall tolerance)", got, want)
	}
}

func TestFinishDelayedWithinTheStallToleranceKeepsThePhoneTime(t *testing.T) {
	r := startIndividualRace(t, 100, 60000, "小明")
	p := r.phones[0]

	r.advanceTo(17000) // a 3 s uplink stall
	p.Finish(100, 14000)
	p.Echo()

	if got, _ := r.host.NextSnapshot(r.h).Player(r.ids[0]); !got.Finished || got.ElapsedMs != 14000 {
		t.Errorf("finish = %+v, want finished at the phone's 14000 ms", got)
	}
}

func TestProgressBeforeTheStartIsIgnored(t *testing.T) {
	h := servertest.Start(t)
	host := h.Host()
	p := h.Phone()
	p.Join("小明")
	host.Start()
	h.Advance(3 * time.Second) // still counting down

	p.Progress(3, 0)
	p.Echo()

	if s := host.NextSnapshot(h); s.Players[0].Total != 0 {
		t.Errorf("total = %d, want 0", s.Players[0].Total)
	}
}

func TestGraceAcceptsOnlyInWindowReports(t *testing.T) {
	r := startRace(t, 15000, "小明")
	p := r.phones[0]

	r.advanceTo(15000 + 500) // 0.5 s into grace
	p.Progress(60, 15100)    // over the duration: the phone should have locked
	p.Progress(40, 15000)    // the final report, delayed by the network
	p.Echo()
	r.advanceTo(15000 + graceMs)
	p.Progress(80, 15000) // arrives after grace: too late

	if res := p.ExpectResult(); res.TotalShakes != 40 {
		t.Errorf("result total = %d, want 40", res.TotalShakes)
	}
	p.Echo() // the late report has been handled
	if got := r.total(t, 0); got != 40 {
		t.Errorf("results snapshot total = %d, want 40", got)
	}
}

func TestRankingByTotalWithEarlierReachWinningTies(t *testing.T) {
	r := startRace(t, 15000, "甲", "乙", "丙")

	r.advanceTo(5000)
	r.phones[0].Progress(30, 5000)
	r.phones[1].Progress(50, 5000)
	r.phones[2].Progress(30, 4000) // reached 30 a second before 甲
	for _, p := range r.phones {
		p.Echo()
	}
	r.advanceTo(15000 + graceMs)

	for i, want := range []int{3, 1, 2} {
		if res := r.phones[i].ExpectResult(); res.Rank != want || res.Players != 3 {
			t.Errorf("phone %d result = %+v, want rank %d of 3", i, res, want)
		}
	}
	s := r.host.NextSnapshot(r.h)
	if s.Phase != "results" || len(s.Top) != 3 {
		t.Fatalf("results snapshot phase %q top %v", s.Phase, s.Top)
	}
	for i, want := range []string{"乙", "丙", "甲"} {
		if got := s.Top[i]; got.Name != want || got.Rank != i+1 {
			t.Errorf("podium %d = %+v, want %s rank %d", i, got, want, i+1)
		}
	}
}

func TestHostSeesEveryRunnerAndALiveTop15(t *testing.T) {
	names := make([]string, 17)
	for i := range names {
		names[i] = fmt.Sprintf("p%d", i)
	}
	r := startRace(t, 30000, names...)

	r.advanceTo(3000)
	for i, p := range r.phones {
		p.Progress(i+1, 3000) // p16 leads with 17
		p.Echo()
	}

	s := r.host.NextSnapshot(r.h)
	if s.Phase != "racing" || len(s.Players) != 17 {
		t.Fatalf("snapshot phase %q with %d players, want racing with 17", s.Phase, len(s.Players))
	}
	if len(s.Top) != 15 {
		t.Fatalf("top has %d entries, want 15", len(s.Top))
	}
	for i, e := range s.Top {
		if wantTotal := 17 - i; e.Total != wantTotal || e.Rank != i+1 {
			t.Errorf("top[%d] = %+v, want total %d rank %d", i, e, wantTotal, i+1)
		}
	}
}

func TestHostSnapshotsArriveEvery100ms(t *testing.T) {
	r := startRace(t, 30000, "小明")
	first := r.host.NextSnapshot(r.h).ServerNow

	for i := 1; i <= 10; i++ {
		if got := r.host.NextSnapshot(r.h).ServerNow; got != first+int64(i)*100 {
			t.Fatalf("snapshot %d at %d, want %d", i, got, first+int64(i)*100)
		}
	}
}

func TestPlayAgainReturnsToTheLobbyWithTheSameRoster(t *testing.T) {
	r := startRace(t, 15000, "小明")
	p := r.phones[0]
	r.advanceTo(1000)
	p.Progress(10, 1000)
	p.Echo()
	r.advanceTo(15000 + graceMs)
	p.ExpectResult()

	r.host.Again()

	s := r.host.NextSnapshot(r.h)
	if s.Phase != "lobby" || len(s.Players) != 1 || s.Players[0].Total != 0 || s.DurationMs != 15000 {
		t.Errorf("after again: phase %q players %+v duration %d, want lobby, 1 player at 0, 15000", s.Phase, s.Players, s.DurationMs)
	}
	r.host.Start()
	p.ExpectCountdown()
}

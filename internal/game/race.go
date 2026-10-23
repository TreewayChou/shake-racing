package game

import "time"

// raceRules: first to Target shakes. Racing ends once the top TopN have
// finished (everyone, in a smaller room), or at TimeCap.
type raceRules struct{}

func (raceRules) ended(r Round, now time.Time, in []*entry) (time.Time, bool) {
	var done []time.Time
	for _, p := range in {
		if p.Finished {
			done = append(done, p.finishedAt)
		}
	}
	if need := min(TopN, len(in)); need > 0 && len(done) >= need {
		// Racing ended at the finish that filled the top: the latest.
		last := done[0]
		for _, t := range done[1:] {
			if t.After(last) {
				last = t
			}
		}
		return last, true
	}
	end := r.StartAt.Add(r.Config.TimeCap)
	return end, !now.Before(end)
}

func (raceRules) counts(r Round, elapsed time.Duration) bool {
	return elapsed <= r.Config.TimeCap
}

// less ranks finishers by their phone-measured finish time, ahead of
// everyone else, who rank as in a timed round: by total, then by who
// reached it first. Equal times go to the earlier joiner.
func (raceRules) less(a, b *entry) bool {
	if a.Finished != b.Finished {
		return a.Finished
	}
	if a.Finished && a.Finish != b.Finish {
		return a.Finish < b.Finish
	}
	return timedRules{}.less(a, b)
}

func (raceRules) target(r Round) int { return r.Config.Target }

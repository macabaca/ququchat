package types

type FinalReviewResult struct {
	Pass        bool     `json:"pass"`
	Score       int      `json:"score"`
	Issues      []string `json:"issues"`
	BetterFinal string   `json:"better_final"`
}

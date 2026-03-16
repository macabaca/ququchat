package agent

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) Add(a, b int64) int64 {
	return a + b
}

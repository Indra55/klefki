package desktop

func (s *Service) ReadIncoming() error {
	s.work.Lock()
	defer s.work.Unlock()
	s.mu.Lock()
	x := s.session
	s.mu.Unlock()
	if x == nil {
		return ErrLocked
	}
	return x.store.ReadIncoming()
}

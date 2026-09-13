package execution

import "context"

func (s *Service) readContent(ctx context.Context, request Request) ([]byte, error) {
	if content, ok := s.content.(RequestContent); ok {
		return content.ReadFor(ctx, s.binding.Token, request, s.cap)
	}
	return s.content.Read(ctx, s.binding.Token, request.InputRef, s.cap)
}

func (s *Service) saveContent(ctx context.Context, request Request, operation string, output, evidence []byte) (string, error) {
	if content, ok := s.content.(RequestContent); ok {
		return content.SaveFor(ctx, s.binding.Token, request, operation, s.cap, output, evidence)
	}
	return s.content.Save(ctx, s.binding.Token, operation, request.InputRef, s.cap, output, evidence)
}

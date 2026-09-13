package contextassembly

import "context"

// DisabledMemories explicitly disables Memory inputs for a fact-only host.
// It grants no optional missing-reference metadata: any candidate is denied.
// Other fact sources still enforce their own current authorization and scope.
type DisabledMemories struct{}

func (DisabledMemories) Load(context.Context, Request, Reference) (Source, error) {
	return Source{}, Denied
}
func (DisabledMemories) Validate(context.Context, Request, Reference, bool) error {
	return Denied
}

package grants

import (
	"encoding/json"
	"google.golang.org/protobuf/encoding/protojson"
	wire "lerna/gen/harness/v1"
	"os"
	"path/filepath"
	"time"
)

type encodedPlan struct {
	Token                  string
	Now                    time.Time
	Request, Spec, Receipt json.RawMessage
}

func writePlan(dir string, p plan) error {
	request, err := protojson.Marshal(p.Request)
	if err != nil {
		return err
	}
	spec, err := protojson.Marshal(p.Spec)
	if err != nil {
		return err
	}
	var receipt []byte
	if p.Receipt != nil {
		receipt, err = protojson.Marshal(p.Receipt)
		if err != nil {
			return err
		}
	}
	data, err := json.Marshal(encodedPlan{p.Token, p.Now, request, spec, receipt})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "plan.json"), data, 0600)
}
func (p *plan) UnmarshalJSON(data []byte) error {
	var in encodedPlan
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	p.Token = in.Token
	p.Now = in.Now
	p.Request = new(wire.GrantMutation)
	p.Spec = new(wire.SignedGrantSpec)
	if err := protojson.Unmarshal(in.Request, p.Request); err != nil {
		return err
	}
	if err := protojson.Unmarshal(in.Spec, p.Spec); err != nil {
		return err
	}
	if len(in.Receipt) > 0 && string(in.Receipt) != "null" {
		p.Receipt = new(wire.GrantReceipt)
		return protojson.Unmarshal(in.Receipt, p.Receipt)
	}
	return nil
}

package decision_engine

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strconv"

	v "github.com/ruipengliu/lerna/contract/v1_1"
)

func preparedV2Digest(p PreparedV2) (string, error) {
	p.Digest = ""
	body, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return hash(append([]byte("lerna-decision-prepared-2\n"), body...)), nil
}

func (p PreparedV2) Validate(record Record) error {
	digest, err := preparedV2Digest(p)
	if err != nil || digest != p.Digest || p.InputDigest != record.InputDigest || p.StartSequence != record.StartSequence || record.Input == nil || p.Artifacts == nil || len(p.Artifacts) > 16 {
		return ErrUnavailable
	}
	key := publicationPrefix(record)
	if p.ProposalKey != key+"/proposal" || hash(p.ProposalBytes) != p.ProposalRef.Hash || strconv.Itoa(len(p.ProposalBytes)) != string(p.ProposalRef.ByteLength) {
		return ErrUnavailable
	}
	output := len(p.ProposalBytes)
	seen := map[v.ContentRef]bool{}
	for i, artifact := range p.Artifacts {
		if artifact.Key != key+"/artifact/"+strconv.Itoa(i) || seen[artifact.Ref] || hash(artifact.Bytes) != artifact.Ref.Hash || strconv.Itoa(len(artifact.Bytes)) != string(artifact.Ref.ByteLength) {
			return ErrUnavailable
		}
		if _, err := v.Encode(artifact.Ref); err != nil {
			return ErrUnavailable
		}
		seen[artifact.Ref] = true
		output += len(artifact.Bytes)
		if output > v.MaxBodyBytes {
			return ErrUnavailable
		}
	}
	encoded, err := v.Encode(p.Proposal)
	if err != nil || !bytes.Equal(encoded, p.ProposalBytes) || p.Proposal.DecisionRef != record.Ref || p.Proposal.SnapshotRef != record.Input.SnapshotRef || !reflect.DeepEqual(p.Sources, p.Proposal.ProcessedSourceRefs) || !withinLimit(output, record.Input.Limits.MaxOutputBytes) {
		return ErrUnavailable
	}
	refs := []v.ContentRef{}
	if candidate, ok := p.Proposal.Advance.AsCandidateResult(); ok {
		refs = candidate.ArtifactRefs
	} else if cannot, ok := p.Proposal.Advance.AsCannotContinue(); ok && cannot.ArtifactRefs != nil {
		refs = *cannot.ArtifactRefs
	}
	if len(refs) != len(p.Artifacts) {
		return ErrUnavailable
	}
	for _, artifact := range p.Artifacts {
		if !slices.Contains(refs, artifact.Ref) {
			return ErrUnavailable
		}
	}
	return nil
}

func publicationPrefix(record Record) string {
	return string(record.Ref.TenantID) + "/" + string(record.Ref.OwnerID) + "/" + string(record.Ref.ID) + "/" + record.InputDigest
}
func (r Record) hasPrepared() bool { return r.Prepared != nil || r.PreparedV2 != nil }
func (r Record) preparedShape() error {
	if r.Prepared != nil && r.PreparedV2 != nil {
		return ErrUnavailable
	}
	if !r.hasPrepared() {
		return nil
	}
	if r.OriginalPermission == nil {
		return ErrUnavailable
	}
	if r.Prepared != nil && r.OriginalPermission.RuleVersion != "fixture-rule/2" {
		return ErrUnavailable
	}
	if r.PreparedV2 != nil && r.OriginalPermission.RuleVersion != "fixture-rule/3" {
		return ErrUnavailable
	}
	return nil
}

type preparedPublication struct {
	key  string
	body []byte
	ref  v.ContentRef
}
type preparedOutput struct {
	digest                string
	proposal              v.Proposal
	proposalRef           v.ContentRef
	sources, artifactRefs []v.ContentRef
	publications          []preparedPublication
}

func (r Record) preparedOutput() (preparedOutput, error) {
	var out preparedOutput
	if err := r.preparedShape(); err != nil || !r.hasPrepared() {
		return out, ErrUnavailable
	}
	if p := r.Prepared; p != nil {
		if err := p.Validate(r); err != nil {
			return out, err
		}
		return preparedOutput{digest: p.Digest, proposal: p.Proposal, proposalRef: p.ProposalRef, sources: p.Sources, artifactRefs: []v.ContentRef{p.ArtifactRef}, publications: []preparedPublication{{p.ArtifactKey, p.ArtifactBytes, p.ArtifactRef}, {p.ProposalKey, p.ProposalBytes, p.ProposalRef}}}, nil
	}
	p := r.PreparedV2
	if err := p.Validate(r); err != nil {
		return out, err
	}
	out = preparedOutput{digest: p.Digest, proposal: p.Proposal, proposalRef: p.ProposalRef, sources: p.Sources, artifactRefs: make([]v.ContentRef, 0, len(p.Artifacts)), publications: make([]preparedPublication, 0, len(p.Artifacts)+1)}
	for _, artifact := range p.Artifacts {
		out.artifactRefs = append(out.artifactRefs, artifact.Ref)
		out.publications = append(out.publications, preparedPublication{artifact.Key, artifact.Bytes, artifact.Ref})
	}
	out.publications = append(out.publications, preparedPublication{p.ProposalKey, p.ProposalBytes, p.ProposalRef})
	return out, nil
}

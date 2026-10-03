package execution

import "github.com/ruipengliu/lerna/api"

func closedContract[I, O any](name, kind string, cas, accepted bool) api.MethodContract {
	c := api.Contract[I, O](name, Namespace, kind, cas, accepted)
	closeSpecialFields(c.InputSchema)
	closeSpecialFields(c.OutputSchema)
	return c
}
func closeSpecialFields(s api.Schema) {
	if p, ok := s["properties"].(map[string]any); ok {
		for name, value := range p {
			child, ok := value.(map[string]any)
			if !ok {
				continue
			}
			switch name {
			case "may_apply_later":
				p[name] = api.Schema{"oneOf": []any{api.Schema{"type": "boolean"}, api.Schema{"const": "unknown"}}}
			case "effect":
				p[name] = api.Enum("not_started", "not_applied", "applied", "unknown")
			case "execution_state":
				p[name] = api.Enum("accepted", "started", "closed")
			default:
				closeSpecialFields(child)
			}
		}
	}
	if items, ok := s["items"].(map[string]any); ok {
		closeSpecialFields(items)
	}
}

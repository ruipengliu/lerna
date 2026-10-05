package grants

import (
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// supportedPermission 对查询分别接纳读取和保存；没有隐含授权。
func supportedPermission(p *v1.PermissionClause) bool {
	return p.UseRight == "INVOKE" || p.Action == "QUERY" && (p.UseRight == "READ" || p.UseRight == "SAVE")
}
func coversCapability(g *v1.Grant, c *v1.Capability, parameters *v1.Ref) bool {
	rights := []string{c.UseRight}
	if c.Action == "QUERY" {
		if c.UseRight != "READ" {
			return false
		}
		rights = append(rights, "SAVE")
	}
	for _, right := range rights {
		matched := false
		for _, p := range g.Permissions {
			if p.Action == c.Action && p.Resource == c.Resource && p.UseRight == right && p.ProcessingPurpose == c.ProcessingPurpose && p.ExecutorEndpointId == c.ExecutorEndpointId && (p.ParameterMode == "ANY" || p.ParameterMode == "EXACT" && proto.Equal(p.ParametersRef, parameters)) {
				matched = true
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

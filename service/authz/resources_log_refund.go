package authz

const ResourceLogRefund = "log_refund"

var (
	LogRefundRead   = Permission{Resource: ResourceLogRefund, Action: ActionRead}
	LogRefundManage = Permission{Resource: ResourceLogRefund, Action: "manage"}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceLogRefund,
		LabelKey: "Abnormal log refund",
		Actions: []ActionDefinition{
			{Action: ActionRead, LabelKey: "View abnormal refund", DescriptionKey: "View the abnormal refund switch, daily status and run history", DefaultRoles: []string{BuiltInRoleAdmin}},
			{Action: "manage", LabelKey: "Manage abnormal refund", DescriptionKey: "Toggle abnormal refund and trigger a refund run for a specific date", DefaultRoles: []string{BuiltInRoleAdmin}},
		},
	})
}

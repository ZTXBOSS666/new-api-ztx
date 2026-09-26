package authz

const ResourceLottery = "lottery"

var (
	LotteryRead   = Permission{Resource: ResourceLottery, Action: ActionRead}
	LotteryManage = Permission{Resource: ResourceLottery, Action: "manage"}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceLottery,
		LabelKey: "Daily lottery",
		Actions: []ActionDefinition{
			{Action: ActionRead, LabelKey: "View lottery names", DescriptionKey: "View unmasked lottery participants and winners", DefaultRoles: []string{BuiltInRoleAdmin}},
			{Action: "manage", LabelKey: "Manage lottery", DescriptionKey: "Configure daily lottery limits and quota rewards", DefaultRoles: []string{BuiltInRoleAdmin}},
		},
	})
}

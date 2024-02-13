package openfgacheck

type Check struct {
	TupleKey             TupleKey `json:"tuple_key"`
	AuthorizationModelID string   `json:"authorization_model_id"`
}

type TupleKey struct {
	User     string `json:"user"`
	Relation string `json:"relation"`
	Object   string `json:"object"`
}

type CheckResponse struct {
	Allowed bool `json:"allowed"`
}

type UserResponse struct {
	ID string `json:"id"`
}

package contracts

// DispatchAuthorization reports whether a claimed run is still authorized to
// begin an external runtime effect. A denied response carries only a stable,
// non-sensitive reason code.
type DispatchAuthorization struct {
	Allowed    bool   `json:"allowed"`
	ReasonCode string `json:"reason_code,omitempty"`
}

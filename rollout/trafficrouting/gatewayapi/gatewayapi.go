package gatewayapi

// Type holds this controller type
const Type = "GatewayAPI"

type ReconcilerConfig struct {

}

type Reconciler struct {}

func NewReconciler(cfg *ReconcilerConfig) *Reconciler {
	reconciler := Reconciler{}
	return &reconciler
}

func (r *Reconciler) SetWeight() {}

func (r *Reconciler) VerifyWeight() {}

func (r *Reconciler) Type() string {
	return Type
}

// Package aws renders translator runs on a GPU rented in AWS for their
// length: it checks every run, launches only when strings need a model —
// wherever it costs least at the time — serves the profile's model through
// vLLM over an SSM tunnel, renders each run against it, and terminates the
// instance on every way out. Setup converges the AWS side a run needs in an
// account: the operator policy, the instance role, and per region a security
// group and the GPU quotas.
package aws

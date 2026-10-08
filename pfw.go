// Package pfw declares dependency graphs for build-time generation.
// Put declarations in files guarded by //go:build pfw_inject, then run
// pfw generate. The generated application uses ordinary constructor calls.
package pfw

// Registration is a build-time declaration read by the generator.
type Registration struct{ marker struct{} }

// Constructors registers named constructor functions, whose parameters are their
// dependencies. Supported results are T, (T, error), and
// (T, func() error, error) for resources that need cleanup.
func Constructors(constructors ...any) Registration { return Registration{} }

// Discover registers functions annotated //pfw:coconut in selected Go package
// patterns. Relative patterns are resolved from the initializer's package.
func Discover(patterns ...string) Registration { return Registration{} }

// Exclude removes constructors from discovery, independent of declaration order.
// Explicit Constructors registrations remain available.
func Exclude(constructors ...any) Registration { return Registration{} }

// AutoBind enables interface inference for providers in its enclosing Module,
// including nested modules. Other modules remain explicit by default.
func AutoBind() Registration { return Registration{} }

// Implementation selects Concrete as the implementation of Interface. The generator
// verifies implementation using Go's type system.
func Implementation[Interface, Concrete any]() Registration { return Registration{} }

// Bind is an alias for Implementation, with the same type argument order.
func Bind[Interface, Concrete any]() Registration { return Implementation[Interface, Concrete]() }

// Module groups static registrations without imposing a package layout.
func Module(registrations ...Registration) Registration { return Registration{} }

// ForEnv activates registrations in the named environment only.
func ForEnv(name Environment, registrations ...Registration) Registration { return Registration{} }

// Environments declares supported names. It is required for initializers with an
// Environment parameter; ForEnv names must belong to this explicit set.
func Environments(names ...Environment) Registration { return Registration{} }

// Override gives wrapped constructors precedence in their scope. Manual interface
// bindings still win; different override candidates remain ambiguous.
func Override(registrations ...Registration) Registration { return Registration{} }

// Build declares the root type for an initializer. Initializer parameters are
// external inputs. Build is a generation marker, never a runtime DI container.
func Build[T any](registrations ...Registration) (T, error) {
	panic("pfw: Build is a generation marker; use a pfw_inject template and run pfw generate")
}

// BuildWithCleanup declares a root whose resources are owned by the initializer.
// On success the generated function returns a cleanup that runs once, in reverse
// construction order. On failure cleanup runs automatically and is returned nil.
// Existing constructors returning T or (T, error) are supported as well.
func BuildWithCleanup[T any](registrations ...Registration) (T, func() error, error) {
	panic("pfw: BuildWithCleanup is a generation marker; use a pfw_inject template and run pfw generate")
}

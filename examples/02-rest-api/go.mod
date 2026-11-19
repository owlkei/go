module example.com/library

go 1.27.4

require go.owlky.com/owlky v0.0.0

require golang.org/x/net v0.62.0 // indirect

// Remove this line once go.owlky.com serves the module.
replace go.owlky.com/owlky => ../..

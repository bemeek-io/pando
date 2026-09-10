// Package planner turns a spec plus host policy plus adapter capabilities into a bundle
// plan, or into a plan-time error.
//
// Everything the planner does is side-effect-free. That boundary is what makes a plan-time
// failure meaningful rather than a label on a mid-deploy crash: steps 1-7 of the deployment
// pipeline create nothing. See design 05 §3.
package planner

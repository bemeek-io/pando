// Package detect runs the detector auction and the trial run.
//
// Ask, never guess (R-102). Every builder adapter bids; the winner emits a draft spec with
// evidence and questions. Questions are held to R-105: answerable by a model that cannot see
// the repo, because the intended workflow is pasting them into the assistant that wrote the
// app. The trial run turns unanswerable questions into observations — watch what the app
// binds rather than asking (R-097). See design 07 sequence A.
package detect

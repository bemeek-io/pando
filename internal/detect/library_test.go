package detect_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/core/spec"
	"github.com/trypando/pando/internal/detect"
)

// TestR021_ALibraryIsNotDeployedAsAnApp asserts R-021.
//
// Flask, chi and a src-layout Python package were read by their manifests as
// web apps and deployed (issue #55). A library has nothing to run, and the
// honest answer is to say so.
func TestR021_ALibraryIsNotDeployedAsAnApp(t *testing.T) {
	for name, src := range map[string]memSource{
		"go library": {
			"go.mod":                  "module github.com/go-chi/chi\n",
			"chi.go":                  "package chi\n",
			"mux.go":                  "// Package chi routes.\npackage chi\n",
			"_examples/hello/main.go": "package main\n",
			"middleware/logger.go":    "package middleware\n",
		},
		"python src layout": {
			"pyproject.toml":          "[project]\nname = \"slugkit\"\n",
			"src/slugkit/__init__.py": "",
			"tests/test_slugify.py":   "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := detect.NewAuction(detect.BuildpackDetector{}).Run(context.Background(), src)
			require.NoError(t, err)
			require.Equal(t, detect.StatusBlocked, result.Status)
			require.Contains(t, result.Blocked.Error(), "library")
		})
	}
}

// TestR132_ARailsAppRunsInProductionAndAsksForItsSecret asserts R-132. In
// development Rails listens on loopback, so nothing could reach it (issue #55);
// production needs SECRET_KEY_BASE, which is the person's to supply.
func TestR132_ARailsAppRunsInProductionAndAsksForItsSecret(t *testing.T) {
	result, err := detect.NewAuction(detect.BuildpackDetector{}).Run(context.Background(), memSource{
		"Gemfile": "gem 'rails'\n", "config/application.rb": "", "bin/rails": "",
	})
	require.NoError(t, err)

	env := map[string]string{}
	for _, e := range result.Winner.Draft.Workloads[0].Env {
		if e.Value != nil {
			env[e.Key] = *e.Value
		}
	}
	require.Equal(t, "production", env["RAILS_ENV"])

	var secretKey *spec.Slot
	for i, s := range result.Winner.Draft.Slots {
		if s.Key == "SECRET_KEY_BASE" {
			secretKey = &result.Winner.Draft.Slots[i]
		}
	}
	require.NotNil(t, secretKey)
	require.True(t, secretKey.Required)
}

// An app is still an app.
func TestAnAppWithAMainPackageOrAnEntryPointIsNotALibrary(t *testing.T) {
	for name, src := range map[string]memSource{
		"go command":     {"go.mod": "module app\n", "cmd/server/main.go": "package main\n", "store.go": "package app\n"},
		"python src app": {"pyproject.toml": "[project]\n", "src/app/__init__.py": "", "Procfile": "web: gunicorn app:app\n"},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := detect.NewAuction(detect.BuildpackDetector{}).Run(context.Background(), src)
			require.NoError(t, err)
			require.NotEqual(t, detect.StatusBlocked, result.Status)
		})
	}
}

// TestR132_ALaravelAppAsksForItsKey asserts R-132. Every Laravel .env.example
// names APP_KEY with no value; left that way the app answers every page with
// "No application encryption key has been specified" (issue #58). The key is
// the person's to supply, so it is a required value, asked for once.
func TestR132_ALaravelAppAsksForItsKey(t *testing.T) {
	result, err := detect.NewAuction(detect.BuildpackDetector{}).Run(context.Background(), memSource{
		"artisan":       "#!/usr/bin/env php\n",
		"composer.json": `{"require": {"php": "^8.3", "laravel/framework": "^13.0"}}`,
		"package.json":  `{"scripts": {"build": "vite build"}}`,
		".env.example":  "APP_NAME=Laravel\nAPP_KEY=\nDB_CONNECTION=sqlite\n",
	})
	require.NoError(t, err)
	draft := result.Winner.Draft

	require.Contains(t, result.Winner.Evidence[0], "PHP", "its package.json builds assets; it is a PHP app")

	count := 0
	for _, e := range draft.Workloads[0].Env {
		if e.Key == "APP_KEY" {
			count++
			require.NotNil(t, e.SlotRef, "the variable is filled from the slot")
		}
	}
	require.Equal(t, 1, count)

	var key *spec.Slot
	for i, s := range draft.Slots {
		if s.Key == "APP_KEY" {
			key = &draft.Slots[i]
		}
	}
	require.NotNil(t, key)
	require.True(t, key.Required)
	require.Contains(t, key.Evidence[0], "php artisan key:generate --show", "the evidence says how to make one")
}

// An APP_KEY .env.example declares empty is asked for once, not kept beside
// the slot, and a Composer package built on Laravel is not an app that needs
// one.
func TestR132_OnlyALaravelAppIsAskedForAKey(t *testing.T) {
	for name, src := range map[string]memSource{
		"declared empty": {
			"artisan": "", "composer.json": `{"require": {"laravel/framework": "^13.0"}}`,
			".env.example": "APP_KEY=\n",
		},
		"package": {
			"composer.json": `{"require": {"laravel/framework": "^13.0"}}`,
			".env.example":  "APP_KEY=\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := detect.NewAuction(detect.BuildpackDetector{}).Run(context.Background(), src)
			require.NoError(t, err)
			slots := 0
			for _, s := range result.Winner.Draft.Slots {
				if s.Key == "APP_KEY" {
					slots++
				}
			}
			if name == "package" {
				require.Zero(t, slots)
			} else {
				require.Equal(t, 1, slots)
			}
		})
	}
}

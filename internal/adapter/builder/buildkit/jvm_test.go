package buildkit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestR095_AJVMProjectIsBuiltWithItsOwnToolOnItsOwnJDK asserts R-095.
//
// nixpacks refused Gradle 9, built Maven on a JDK the project did not ask for,
// and had no plan for Java with no build tool (issue #55).
func TestR095_AJVMProjectIsBuiltWithItsOwnToolOnItsOwnJDK(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		want  jvmBuild
	}{
		"gradle toolchain": {
			files: map[string]string{"gradlew": "", "build.gradle": "java { toolchain { languageVersion = JavaLanguageVersion.of(17) } }"},
			want:  jvmBuild{Tool: "gradle", JDK: 17},
		},
		"maven java.version": {
			files: map[string]string{"pom.xml": "<properties><java.version>24</java.version></properties>"},
			want:  jvmBuild{Tool: "maven", JDK: 25},
		},
		"maven with nothing declared": {
			files: map[string]string{"pom.xml": "<project/>"},
			want:  jvmBuild{Tool: "maven", JDK: jvmDefault},
		},
		"plain java": {
			files: map[string]string{"Main.java": "public class Main { public static void main(String[] a) {} }"},
			want:  jvmBuild{Tool: "javac", JDK: jvmDefault, MainClass: "Main"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := readJVMBuild(writeFiles(t, tc.files))
			require.True(t, ok)
			require.Equal(t, tc.want, got)
		})
	}

	_, ok := readJVMBuild(writeFiles(t, map[string]string{"Lib.java": "package x; public class Lib {}"}))
	require.False(t, ok, "a package tree with no build file is not guessed at")
}

func TestAJVMPlanStartsThroughAShellLikeEveryPlan(t *testing.T) {
	root := t.TempDir()
	name, err := writeJVMPlan(root, jvmBuild{Tool: "maven", JDK: 21})
	require.NoError(t, err)
	body, err := os.ReadFile(filepath.Join(root, name))
	require.NoError(t, err)
	require.Contains(t, string(body), "FROM maven:3-eclipse-temurin-21 AS build")
	require.Contains(t, string(body), `ENTRYPOINT ["/bin/sh", "-c"]`)
	require.Contains(t, string(body), `CMD ["exec java -jar /app/app.jar"]`)
}

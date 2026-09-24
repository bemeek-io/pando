package buildkit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/errs"
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

// A JDK newer than any published image is built on the newest one, which is
// the closest thing to what the project asked for.
func TestAJDKNewerThanAnyReleaseGetsTheNewest(t *testing.T) {
	b, ok := readJVMBuild(writeFiles(t, map[string]string{
		"gradlew": "", "build.gradle.kts": "java { sourceCompatibility = JavaVersion.VERSION_99 }",
	}))
	require.True(t, ok)
	require.Equal(t, jvmBuild{Tool: "gradle", JDK: jvmReleases[len(jvmReleases)-1]}, b)
}

// Java sources with no class that has a main have nothing to run, and are
// left to nixpacks.
func TestJavaSourcesWithoutAMainAreNotPlanned(t *testing.T) {
	_, ok := readJVMBuild(writeFiles(t, map[string]string{"Util.java": "public class Util { static int x() { return 1; } }"}))
	require.False(t, ok)
}

// Each build tool gets its own plan, and each starts through a shell. The
// javac plan runs the main class by name, so a name that is not a Java class
// is refused before it reaches the Dockerfile, as is a tool Pando has no plan
// for.
func TestEachJVMBuildToolHasItsOwnPlan(t *testing.T) {
	gradle := t.TempDir()
	name, err := writeJVMPlan(gradle, jvmBuild{Tool: "gradle", JDK: 17})
	require.NoError(t, err)
	body, err := os.ReadFile(filepath.Join(gradle, name))
	require.NoError(t, err)
	require.Contains(t, string(body), "FROM eclipse-temurin:17-jdk AS build")
	require.Contains(t, string(body), "./gradlew --no-daemon bootJar -x test")
	require.Contains(t, string(body), "FROM eclipse-temurin:17-jre")

	javac := t.TempDir()
	name, err = writeJVMPlan(javac, jvmBuild{Tool: "javac", JDK: 21, MainClass: "Main"})
	require.NoError(t, err)
	body, err = os.ReadFile(filepath.Join(javac, name))
	require.NoError(t, err)
	require.Contains(t, string(body), "javac -d /app/classes *.java")
	require.Contains(t, string(body), `CMD ["exec java -cp /app/classes Main"]`)

	_, err = writeJVMPlan(t.TempDir(), jvmBuild{Tool: "javac", JDK: 21, MainClass: "Main; rm -rf /"})
	require.Equal(t, errs.BuildFailed, errs.CodeOf(err))
	require.Contains(t, errs.As(err).Message, "is not a Java class name")

	_, err = writeJVMPlan(t.TempDir(), jvmBuild{Tool: "sbt", JDK: 21})
	require.Equal(t, errs.BuildFailed, errs.CodeOf(err))
	require.Contains(t, errs.As(err).Message, "sbt")
}

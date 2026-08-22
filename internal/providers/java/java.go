package java

import (
	"regexp"

	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/provider"
)

type JavaProvider struct {
	buildTool string
	framework string
	version   string
}

func (p *JavaProvider) Name() string { return "java" }

func (p *JavaProvider) Detect(ctx *provider.DetectContext) (bool, error) {
	return ctx.App.HasFile("pom.xml") || ctx.App.HasFile("gradlew") ||
		ctx.App.HasFile("build.gradle") || ctx.App.HasFile("build.gradle.kts"), nil
}

func (p *JavaProvider) Initialize(ctx *provider.DetectContext) error {
	if ctx.App.HasFile("gradlew") || ctx.App.HasFile("build.gradle") || ctx.App.HasFile("build.gradle.kts") {
		p.buildTool = "gradle"
	} else {
		p.buildTool = "maven"
	}
	p.framework = "java"
	if ctx.App.HasFile("pom.xml") && ctx.App.HasFileWithContent("pom.xml", "spring-boot") {
		p.framework = "spring-boot"
	}
	p.version = "21"
	if ctx.App.HasFile("pom.xml") {
		if content, err := ctx.App.ReadFileString("pom.xml"); err == nil {
			re := regexp.MustCompile(`<java.version>(\d+)</java.version>`)
			if m := re.FindStringSubmatch(content); len(m) > 1 {
				p.version = m[1]
			}
		}
	}
	return nil
}

func (p *JavaProvider) Plan(ctx *provider.DetectContext) (*buildplan.Plan, error) {
	plan := buildplan.NewDefaultPlan()
	plan.Provider = "java"
	plan.DetectedFramework = p.framework
	plan.Runtime = p.version
	plan.Port = 8080
	plan.Env = map[string]string{"JAVA_OPTS": "-XX:+UseContainerSupport"}

	if p.buildTool == "gradle" {
		plan.BuildCmd = "./gradlew build -x test"
		plan.StartCmd = "java -jar build/libs/*.jar"
	} else {
		buildPrefix := "mvn"
		if ctx.App.HasFile("mvnw") {
			buildPrefix = "./mvnw"
		}
		plan.BuildCmd = buildPrefix + " package -DskipTests"
		plan.StartCmd = "java -jar target/*.jar"
	}
	return plan, nil
}

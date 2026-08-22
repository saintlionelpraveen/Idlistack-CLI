// Package python implements the Python provider.
// Detects: Django, Flask, FastAPI, Gunicorn, Uvicorn, and generic Python apps.
package python

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/provider"
)

type PythonProvider struct {
	framework  string
	version    string
	hasPipfile bool
	hasPoetry  bool
}

func (p *PythonProvider) Name() string {
	return "python"
}

func (p *PythonProvider) Detect(ctx *provider.DetectContext) (bool, error) {
	return ctx.App.HasFile("requirements.txt") ||
		ctx.App.HasFile("pyproject.toml") ||
		ctx.App.HasFile("Pipfile") ||
		ctx.App.HasFile("setup.py") ||
		ctx.App.HasFile("main.py") ||
		ctx.App.HasFile("app.py") ||
		ctx.App.HasFile("manage.py"), nil
}

func (p *PythonProvider) Initialize(ctx *provider.DetectContext) error {
	p.hasPipfile = ctx.App.HasFile("Pipfile")
	p.hasPoetry = ctx.App.HasFile("pyproject.toml") && ctx.App.HasFileWithContent("pyproject.toml", "[tool.poetry]")
	p.framework = p.detectFramework(ctx)
	p.version = p.detectVersion(ctx)
	return nil
}

func (p *PythonProvider) Plan(ctx *provider.DetectContext) (*buildplan.Plan, error) {
	plan := buildplan.NewDefaultPlan()
	plan.Provider = "python"
	plan.DetectedFramework = p.framework
	plan.Runtime = p.version

	// Install command
	if p.hasPoetry {
		plan.InstallCmd = "pip install poetry && poetry install --no-interaction --no-ansi"
	} else if p.hasPipfile {
		plan.InstallCmd = "pip install pipenv && pipenv install --deploy --system"
	} else if ctx.App.HasFile("requirements.txt") {
		plan.InstallCmd = "pip install -r requirements.txt"
	} else if ctx.App.HasFile("pyproject.toml") {
		plan.InstallCmd = "pip install ."
	}

	// Framework-specific configuration
	switch p.framework {
	case "django":
		plan.Port = 8000
		if ctx.App.HasFile("manage.py") {
			plan.StartCmd = "python manage.py runserver 0.0.0.0:8000"
			// Production: prefer gunicorn
			if p.hasDependency(ctx, "gunicorn") {
				wsgiModule := p.findDjangoWSGI(ctx)
				plan.StartCmd = fmt.Sprintf("gunicorn %s --bind 0.0.0.0:8000", wsgiModule)
			}
		}
		plan.Env = map[string]string{
			"PYTHONDONTWRITEBYTECODE": "1",
			"PYTHONUNBUFFERED":        "1",
		}
	case "flask":
		plan.Port = 5000
		plan.StartCmd = "gunicorn app:app --bind 0.0.0.0:5000"
		if !p.hasDependency(ctx, "gunicorn") {
			plan.StartCmd = "flask run --host=0.0.0.0 --port=5000"
		}
		plan.Env = map[string]string{
			"FLASK_APP":               "app.py",
			"PYTHONDONTWRITEBYTECODE": "1",
			"PYTHONUNBUFFERED":        "1",
		}
	case "fastapi":
		plan.Port = 8000
		plan.StartCmd = "uvicorn main:app --host 0.0.0.0 --port 8000"
		if ctx.App.HasFile("app.py") && !ctx.App.HasFile("main.py") {
			plan.StartCmd = "uvicorn app:app --host 0.0.0.0 --port 8000"
		}
		plan.Env = map[string]string{
			"PYTHONDONTWRITEBYTECODE": "1",
			"PYTHONUNBUFFERED":        "1",
		}
	case "streamlit":
		plan.Port = 8501
		entryFile := "app.py"
		if ctx.App.HasFile("streamlit_app.py") {
			entryFile = "streamlit_app.py"
		}
		plan.StartCmd = fmt.Sprintf("streamlit run %s --server.port=8501 --server.address=0.0.0.0", entryFile)
	default:
		plan.Port = 8000
		if ctx.App.HasFile("main.py") {
			plan.StartCmd = "python main.py"
		} else if ctx.App.HasFile("app.py") {
			plan.StartCmd = "python app.py"
		} else {
			plan.StartCmd = "python -m app"
		}
		plan.Env = map[string]string{
			"PYTHONDONTWRITEBYTECODE": "1",
			"PYTHONUNBUFFERED":        "1",
		}
	}

	return plan, nil
}

func (p *PythonProvider) detectFramework(ctx *provider.DetectContext) string {
	// Django — manage.py is the canonical indicator
	if ctx.App.HasFile("manage.py") {
		return "django"
	}

	// FastAPI
	if p.hasDependency(ctx, "fastapi") {
		return "fastapi"
	}

	// Flask
	if p.hasDependency(ctx, "flask") || p.hasDependency(ctx, "Flask") {
		return "flask"
	}

	// Streamlit
	if p.hasDependency(ctx, "streamlit") {
		return "streamlit"
	}

	return "python"
}

func (p *PythonProvider) detectVersion(ctx *provider.DetectContext) string {
	// Check runtime.txt (Heroku convention)
	if ctx.App.HasFile("runtime.txt") {
		if content, err := ctx.App.ReadFileString("runtime.txt"); err == nil {
			re := regexp.MustCompile(`python-(\d+\.\d+)`)
			if matches := re.FindStringSubmatch(strings.TrimSpace(content)); len(matches) > 1 {
				return matches[1]
			}
		}
	}

	// Check pyproject.toml for requires-python
	if ctx.App.HasFile("pyproject.toml") {
		if content, err := ctx.App.ReadFileString("pyproject.toml"); err == nil {
			re := regexp.MustCompile(`requires-python\s*=\s*["><=]*\s*(\d+\.\d+)`)
			if matches := re.FindStringSubmatch(content); len(matches) > 1 {
				return matches[1]
			}
		}
	}

	return "3"
}

func (p *PythonProvider) hasDependency(ctx *provider.DetectContext, name string) bool {
	// Check requirements.txt
	if ctx.App.HasFile("requirements.txt") {
		if content, err := ctx.App.ReadFileString("requirements.txt"); err == nil {
			lines := strings.Split(content, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(strings.ToLower(line), strings.ToLower(name)) {
					return true
				}
			}
		}
	}

	// Check pyproject.toml
	if ctx.App.HasFile("pyproject.toml") {
		return ctx.App.HasFileWithContent("pyproject.toml", name)
	}

	// Check Pipfile
	if ctx.App.HasFile("Pipfile") {
		return ctx.App.HasFileWithContent("Pipfile", name)
	}

	return false
}

func (p *PythonProvider) findDjangoWSGI(ctx *provider.DetectContext) string {
	// Try to find WSGI module from manage.py or settings
	if ctx.App.HasFile("manage.py") {
		if content, err := ctx.App.ReadFileString("manage.py"); err == nil {
			re := regexp.MustCompile(`DJANGO_SETTINGS_MODULE.*?["']([^"']+)["']`)
			if matches := re.FindStringSubmatch(content); len(matches) > 1 {
				// settings module like "myapp.settings" → wsgi is "myapp.wsgi"
				parts := strings.Split(matches[1], ".")
				if len(parts) > 0 {
					return parts[0] + ".wsgi:application"
				}
			}
		}
	}
	return "app.wsgi:application"
}

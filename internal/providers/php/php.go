// Package php implements the PHP provider.
// Detects and configures: Laravel, Symfony, WordPress, CodeIgniter, CakePHP, Drupal, and generic PHP apps.
package php

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/idlistack/cli/internal/buildplan"
	"github.com/idlistack/cli/internal/provider"
)

type PhpProvider struct {
	framework        string
	frameworkVersion string
	phpVersion       string
	documentRoot     string
}

func (p *PhpProvider) Name() string {
	return "php"
}

func (p *PhpProvider) Detect(ctx *provider.DetectContext) (bool, error) {
	return ctx.App.HasFile("composer.json") ||
		ctx.App.HasFile("index.php") ||
		ctx.App.HasFile("artisan") ||
		ctx.App.HasFile("public/index.php") ||
		ctx.App.HasFile("wp-config.php") ||
		ctx.App.HasFile("wp-settings.php"), nil
}

func (p *PhpProvider) Initialize(ctx *provider.DetectContext) error {
	p.framework, p.frameworkVersion = p.detectFramework(ctx)
	p.phpVersion = p.detectPhpVersion(ctx)
	p.documentRoot = p.detectDocumentRoot(ctx)
	return nil
}

func (p *PhpProvider) Plan(ctx *provider.DetectContext) (*buildplan.Plan, error) {
	plan := buildplan.NewDefaultPlan()
	plan.Provider = "php"
	plan.DetectedFramework = p.framework
	plan.Framework = p.framework
	plan.FrameworkVersion = p.frameworkVersion
	plan.Runtime = p.phpVersion
	plan.Stack = "PHP"
	plan.StackVersion = p.phpVersion
	plan.StaticDir = p.documentRoot
	plan.Port = 8080

	// Dynamically detect required PHP extensions and apt packages
	extensions, aptDeps := p.detectExtensions(ctx)

	// Always ensure core utilities are available
	aptSet := make(map[string]bool)
	for _, apt := range aptDeps {
		aptSet[apt] = true
	}
	aptSet["socat"] = true // Universal transparent database loopback proxy
	aptSet["unzip"] = true
	aptSet["curl"] = true

	var uniqueApts []string
	for apt := range aptSet {
		uniqueApts = append(uniqueApts, apt)
	}

	var preInstallSteps []string
	if len(uniqueApts) > 0 {
		preInstallSteps = append(preInstallSteps, fmt.Sprintf("apt-get update && apt-get install -y --no-install-recommends %s", strings.Join(uniqueApts, " ")))
	}

	// Install Composer if composer.json exists
	if ctx.App.HasFile("composer.json") {
		preInstallSteps = append(preInstallSteps, "(command -v composer >/dev/null 2>&1 || curl -sS https://getcomposer.org/installer | php -- --install-dir=/usr/local/bin --filename=composer)")
	}

	// Add docker-php-ext-configure flags if required
	for _, ext := range extensions {
		if ext == "gd" {
			preInstallSteps = append(preInstallSteps, "docker-php-ext-configure gd --with-freetype --with-jpeg --with-webp")
		}
	}

	if len(extensions) > 0 {
		preInstallSteps = append(preInstallSteps, fmt.Sprintf("docker-php-ext-install -j$(nproc) %s", strings.Join(extensions, " ")))
	}

	// Configure Apache: Port 8080, mod_rewrite, mod_headers, mod_setenvif, AllowOverride All, and DocumentRoot
	apacheConfigSteps := []string{
		"sed -i 's/80/8080/g' /etc/apache2/ports.conf /etc/apache2/sites-available/*.conf",
		"a2enmod rewrite headers setenvif",
		"sed -i 's/AllowOverride None/AllowOverride All/g' /etc/apache2/apache2.conf",
		"echo 'SetEnvIf Request_URI \".*\" HTTPS=on' > /etc/apache2/conf-available/idlistack-ssl.conf && a2enconf idlistack-ssl",
	}
	if p.documentRoot != "" {
		apacheConfigSteps = append(apacheConfigSteps, fmt.Sprintf("sed -ri -e 's!/var/www/html!/var/www/html/%s!g' /etc/apache2/sites-available/*.conf /etc/apache2/apache2.conf", p.documentRoot))
	}
	preInstallSteps = append(preInstallSteps, strings.Join(apacheConfigSteps, " && "))

	// Create production php.ini settings and entrypoint script with automatic .htaccess HTTPS redirect sanitizer
	iniAndEntrypoint := `echo "mysqli.default_host = db\nmysqli.default_port = 3306\npdo_mysql.default_socket = /var/run/mysqld/mysqld.sock\nupload_max_filesize = 64M\npost_max_size = 64M\nmemory_limit = 256M\nmax_execution_time = 300" > /usr/local/etc/php/conf.d/idlistack.ini && echo '#!/bin/sh\nmkdir -p /var/run/mysqld && chmod 777 /var/run/mysqld\nDB_TARGET="${DB_HOST:-db}"\nsocat TCP-LISTEN:3306,fork,bind=127.0.0.1,reuseaddr TCP:$DB_TARGET:3306 2>/dev/null &\nsocat UNIX-LISTEN:/var/run/mysqld/mysqld.sock,fork,mode=777 TCP:$DB_TARGET:3306 2>/dev/null &\nfind /var/www/html -maxdepth 2 -name ".htaccess" -exec sed -i -E "s/^[[:space:]]*(RewriteCond[[:space:]]+%{HTTPS}[[:space:]]+off)/# \1/gI" {} + 2>/dev/null || true\nfind /var/www/html -maxdepth 2 -name ".htaccess" -exec sed -i -E "s/^[[:space:]]*(RewriteRule[[:space:]]+.*https:\/\/)/# \1/gI" {} + 2>/dev/null || true\nexec apache2-foreground "$@"' > /entrypoint.sh && chmod +x /entrypoint.sh`
	preInstallSteps = append(preInstallSteps, iniAndEntrypoint)

	if len(preInstallSteps) > 0 {
		plan.PreInstallCmd = strings.Join(preInstallSteps, " && ")
	}

	switch p.framework {
	case "laravel":
		plan.InstallCmd = "composer install --no-dev --optimize-autoloader --no-interaction"
		plan.BuildCmd = "php artisan config:cache && php artisan route:cache && php artisan view:cache || true"
		plan.StartCmd = "/entrypoint.sh"
		plan.PreDeployCmd = "php artisan migrate --force || true"
		plan.Env = map[string]string{
			"APP_ENV": "production",
		}
	case "symfony":
		plan.InstallCmd = "composer install --no-dev --optimize-autoloader --no-interaction"
		plan.BuildCmd = "php bin/console cache:clear --env=prod || true"
		plan.StartCmd = "/entrypoint.sh"
	case "wordpress":
		plan.StartCmd = "/entrypoint.sh"
	default:
		if ctx.App.HasFile("composer.json") {
			plan.InstallCmd = "composer install --no-dev --optimize-autoloader --no-interaction || composer install"
		}
		plan.StartCmd = "/entrypoint.sh"
	}

	// Detect if MySQL/MariaDB database is used
	needsMysql := false
	for _, ext := range extensions {
		if ext == "mysqli" || ext == "pdo_mysql" {
			needsMysql = true
			break
		}
	}
	if !needsMysql && (ctx.App.HasFile("database.sql") || ctx.App.HasFile("schema.sql") || ctx.App.HasFile("init.sql")) {
		needsMysql = true
	}
	if needsMysql {
		plan.Dependencies = append(plan.Dependencies, "mariadb:10.6")
	}

	// Detect if PostgreSQL database is used
	for _, ext := range extensions {
		if ext == "pdo_pgsql" || ext == "pgsql" {
			plan.Dependencies = append(plan.Dependencies, "postgres:15-alpine")
			break
		}
	}

	plan.Normalize()
	return plan, nil
}

func (p *PhpProvider) detectFramework(ctx *provider.DetectContext) (string, string) {
	if ctx.App.HasFile("artisan") {
		version := extractFrameworkVersionFromLock(ctx, "laravel/framework")
		return "laravel", version
	}
	if ctx.App.HasFile("bin/console") && ctx.App.HasFile("symfony.lock") {
		version := extractFrameworkVersionFromLock(ctx, "symfony/framework-bundle")
		return "symfony", version
	}
	if ctx.App.HasFile("wp-config.php") || ctx.App.HasFile("wp-settings.php") {
		version := ""
		if b, err := os.ReadFile(filepath.Join(ctx.App.Source, "wp-includes", "version.php")); err == nil {
			re := regexp.MustCompile(`\$wp_version\s*=\s*['"]([^'"]+)['"]`)
			if matches := re.FindStringSubmatch(string(b)); len(matches) > 1 {
				version = matches[1]
			}
		}
		return "wordpress", version
	}
	if ctx.App.HasFile("system/core/CodeIgniter.php") || ctx.App.HasFile("app/Config/App.php") {
		return "codeigniter", ""
	}
	if ctx.App.HasFile("config/cakephp.php") || ctx.App.HasFile("src/Application.php") {
		return "cakephp", ""
	}
	if ctx.App.HasFile("core/lib/Drupal.php") || ctx.App.HasFile("sites/default/settings.php") {
		return "drupal", ""
	}
	return "php", ""
}

func (p *PhpProvider) detectDocumentRoot(ctx *provider.DetectContext) string {
	if ctx.App.HasFile("public/index.php") {
		return "public"
	}
	if ctx.App.HasFile("web/index.php") {
		return "web"
	}
	if ctx.App.HasFile("public_html/index.php") {
		return "public_html"
	}
	if ctx.App.HasFile("htdocs/index.php") {
		return "htdocs"
	}
	return ""
}

func (p *PhpProvider) detectPhpVersion(ctx *provider.DetectContext) string {
	// 1. Check .php-version file
	if b, err := os.ReadFile(filepath.Join(ctx.App.Source, ".php-version")); err == nil {
		v := strings.TrimSpace(string(b))
		if v != "" {
			return normalizePhpVersion(v)
		}
	}

	// 2. Check composer.json require.php
	if ctx.App.HasFile("composer.json") {
		var comp struct {
			Require map[string]string `json:"require"`
		}
		if err := ctx.App.ReadJSON("composer.json", &comp); err == nil {
			if reqPhp, ok := comp.Require["php"]; ok {
				v := extractVersionFromConstraint(reqPhp)
				if v != "" {
					return v
				}
			}
		}
	}

	// 3. Check composer.lock platform.php
	if ctx.App.HasFile("composer.lock") {
		var lock struct {
			Platform struct {
				Php string `json:"php"`
			} `json:"platform"`
			PlatformOverrides struct {
				Php string `json:"php"`
			} `json:"platform-overrides"`
		}
		if err := ctx.App.ReadJSON("composer.lock", &lock); err == nil {
			if lock.PlatformOverrides.Php != "" {
				return normalizePhpVersion(lock.PlatformOverrides.Php)
			}
			if lock.Platform.Php != "" {
				return normalizePhpVersion(lock.Platform.Php)
			}
		}
	}

	// Default to modern stable production PHP 8.2
	return "8.2"
}

func normalizePhpVersion(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "v")
	re := regexp.MustCompile(`^(\d+\.\d+)`)
	if matches := re.FindStringSubmatch(raw); len(matches) > 1 {
		majorMinor := matches[1]
		switch majorMinor {
		case "8.3", "8.2", "8.1", "8.0", "7.4":
			return majorMinor
		}
	}
	if strings.HasPrefix(raw, "8") {
		return "8.2"
	}
	if strings.HasPrefix(raw, "7") {
		return "7.4"
	}
	return "8.2"
}

func extractVersionFromConstraint(constraint string) string {
	// Matches: ^8.2, >=8.1, ~8.0, 7.4.*, 8.2
	re := regexp.MustCompile(`(\d+\.\d+)`)
	matches := re.FindAllString(constraint, -1)
	if len(matches) > 0 {
		// Pick highest matched valid version
		for i := len(matches) - 1; i >= 0; i-- {
			norm := normalizePhpVersion(matches[i])
			if norm != "" {
				return norm
			}
		}
	}
	return ""
}

func extractFrameworkVersionFromLock(ctx *provider.DetectContext, pkgName string) string {
	if !ctx.App.HasFile("composer.lock") {
		return ""
	}
	var lock struct {
		Packages []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"packages"`
	}
	if err := ctx.App.ReadJSON("composer.lock", &lock); err == nil {
		for _, pkg := range lock.Packages {
			if pkg.Name == pkgName {
				return strings.TrimPrefix(pkg.Version, "v")
			}
		}
	}
	return ""
}

// detectExtensions scans the source code to dynamically figure out which PHP extensions
// and apt dependencies need to be installed in the Docker image.
func (p *PhpProvider) detectExtensions(ctx *provider.DetectContext) ([]string, []string) {
	extSet := make(map[string]bool)
	aptSet := make(map[string]bool)

	// Framework defaults
	if p.framework == "laravel" || p.framework == "symfony" {
		extSet["pdo_mysql"] = true
		extSet["pdo_pgsql"] = true
		extSet["bcmath"] = true
		extSet["zip"] = true
		extSet["opcache"] = true
		extSet["intl"] = true
		aptSet["libpq-dev"] = true
		aptSet["libzip-dev"] = true
		aptSet["libicu-dev"] = true
	} else if p.framework == "wordpress" {
		extSet["mysqli"] = true
		extSet["pdo_mysql"] = true
		extSet["gd"] = true
		extSet["zip"] = true
		extSet["opcache"] = true
		aptSet["libpng-dev"] = true
		aptSet["libjpeg-dev"] = true
		aptSet["libfreetype6-dev"] = true
		aptSet["libwebp-dev"] = true
		aptSet["libzip-dev"] = true
	}

	// 1. Scan composer.json for "ext-*" dependencies
	if ctx.App.HasFile("composer.json") {
		var composer struct {
			Require map[string]string `json:"require"`
		}
		if err := ctx.App.ReadJSON("composer.json", &composer); err == nil {
			for req := range composer.Require {
				if strings.HasPrefix(req, "ext-") {
					extName := strings.ToLower(strings.TrimPrefix(req, "ext-"))
					extSet[extName] = true
				}
			}
		}
	}

	// 2. Scan PHP source files for function/class usages
	mysqliRe := regexp.MustCompile(`(?i)(new\s+mysqli|mysqli_connect\()`)
	pdoMysqlRe := regexp.MustCompile(`(?i)new\s+PDO\s*\(\s*['"]mysql:`)
	pdoPgsqlRe := regexp.MustCompile(`(?i)new\s+PDO\s*\(\s*['"]pgsql:`)
	pdoSqliteRe := regexp.MustCompile(`(?i)new\s+PDO\s*\(\s*['"]sqlite:`)
	gdRe := regexp.MustCompile(`(?i)(imagecreate|imagejpeg|imagepng|imagewebp)`)
	zipRe := regexp.MustCompile(`(?i)new\s+ZipArchive`)
	intlRe := regexp.MustCompile(`(?i)(IntlDateFormatter|NumberFormatter|Collator)`)
	bcmathRe := regexp.MustCompile(`(?i)(bcadd|bcsub|bcmul|bcdiv)`)
	soapRe := regexp.MustCompile(`(?i)(SoapClient|SoapServer)`)

	_ = filepath.Walk(ctx.App.Source, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".php") {
			return nil
		}

		// Skip vendor and cache directories to save time
		if strings.Contains(path, "/vendor/") || strings.Contains(path, "\\vendor\\") ||
			strings.Contains(path, "/cache/") || strings.Contains(path, "/storage/") {
			return nil
		}

		contentBytes, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(contentBytes)

		if mysqliRe.MatchString(content) {
			extSet["mysqli"] = true
		}
		if pdoMysqlRe.MatchString(content) {
			extSet["pdo_mysql"] = true
		}
		if pdoPgsqlRe.MatchString(content) {
			extSet["pdo_pgsql"] = true
			aptSet["libpq-dev"] = true
		}
		if pdoSqliteRe.MatchString(content) {
			extSet["pdo_sqlite"] = true
			aptSet["libsqlite3-dev"] = true
		}
		if gdRe.MatchString(content) {
			extSet["gd"] = true
			aptSet["libpng-dev"] = true
			aptSet["libjpeg-dev"] = true
			aptSet["libfreetype6-dev"] = true
			aptSet["libwebp-dev"] = true
		}
		if zipRe.MatchString(content) {
			extSet["zip"] = true
			aptSet["libzip-dev"] = true
		}
		if intlRe.MatchString(content) {
			extSet["intl"] = true
			aptSet["libicu-dev"] = true
		}
		if bcmathRe.MatchString(content) {
			extSet["bcmath"] = true
		}
		if soapRe.MatchString(content) {
			extSet["soap"] = true
			aptSet["libxml2-dev"] = true
		}

		return nil
	})

	// Also check for database.sql, schema.sql, or *.sql
	if _, err := os.Stat(filepath.Join(ctx.App.Source, "database.sql")); err == nil {
		extSet["mysqli"] = true
		extSet["pdo_mysql"] = true
	}

	var exts []string
	for ext := range extSet {
		// Skip extensions that are already compiled into official PHP Apache images
		if ext == "json" || ext == "mbstring" || ext == "xml" || ext == "ctype" ||
			ext == "tokenizer" || ext == "session" || ext == "dom" || ext == "pcre" ||
			ext == "filter" || ext == "hash" || ext == "spl" || ext == "standard" {
			continue
		}
		exts = append(exts, ext)
	}

	var apts []string
	for apt := range aptSet {
		apts = append(apts, apt)
	}

	return exts, apts
}


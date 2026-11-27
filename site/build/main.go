// Command build renders the NeoSwitch landing page in every supported language.
//
// It reads site/template.html and one site/i18n/<code>.json file per language, writes index.html for English at the output root and <code>/index.html for every other language, writes a sitemap with hreflang alternates, and copies site/public as is. Run it from the repository root: go run ./site/build -out _site
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

const origin = "https://neo-switch.yldm.ai"

// Lang describes one published language of the landing page.
type Lang struct {
	Code     string // BCP 47 tag used for <html lang>, hreflang and the i18n file name
	OGLocale string // Open Graph locale, e.g. zh_CN
	Name     string // native language name shown in the switcher
	Path     string // site path, "/" for the default language
	URL      string // absolute URL
}

// langs lists every published language; the first entry is the default served at "/". The set matches the README translations under i18n/.
var langs = []Lang{
	{Code: "en", OGLocale: "en_US", Name: "English"},
	{Code: "zh-CN", OGLocale: "zh_CN", Name: "简体中文"},
	{Code: "zh-TW", OGLocale: "zh_TW", Name: "繁體中文"},
	{Code: "ja", OGLocale: "ja_JP", Name: "日本語"},
	{Code: "ko", OGLocale: "ko_KR", Name: "한국어"},
	{Code: "es", OGLocale: "es_ES", Name: "Español"},
	{Code: "fr", OGLocale: "fr_FR", Name: "Français"},
	{Code: "de", OGLocale: "de_DE", Name: "Deutsch"},
	{Code: "pt-BR", OGLocale: "pt_BR", Name: "Português (Brasil)"},
	{Code: "ru", OGLocale: "ru_RU", Name: "Русский"},
}

func init() {
	for i := range langs {
		if i == 0 {
			langs[i].Path = "/"
		} else {
			langs[i].Path = "/" + langs[i].Code + "/"
		}
		langs[i].URL = origin + langs[i].Path
	}
}

type page struct {
	Lang    Lang
	Langs   []Lang
	Default Lang
	Origin  string
	T       map[string]template.HTML
	JSONLD  template.JS
}

func main() {
	src := flag.String("src", "site", "directory holding template.html, i18n/ and public/")
	out := flag.String("out", "_site", "output directory")
	flag.Parse()
	if err := build(*src, *out); err != nil {
		log.Fatal(err)
	}
}

func build(src, out string) error {
	tpl, err := template.ParseFiles(filepath.Join(src, "template.html"))
	if err != nil {
		return err
	}
	strings, err := loadStrings(filepath.Join(src, "i18n"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for _, l := range langs {
		html, err := render(tpl, l, strings[l.Code])
		if err != nil {
			return fmt.Errorf("%s: %w", l.Code, err)
		}
		dir := filepath.Join(out, filepath.FromSlash(l.Path))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "index.html"), html, 0o644); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(out, "sitemap.xml"), sitemap(), 0o644); err != nil {
		return err
	}
	return copyDir(filepath.Join(src, "public"), out)
}

// loadStrings reads every language file and checks that each one has exactly the keys of the default language, so a missing or stray translation fails the build instead of rendering an empty string.
func loadStrings(dir string) (map[string]map[string]string, error) {
	all := make(map[string]map[string]string, len(langs))
	for _, l := range langs {
		b, err := os.ReadFile(filepath.Join(dir, l.Code+".json"))
		if err != nil {
			return nil, err
		}
		m := map[string]string{}
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("%s.json: %w", l.Code, err)
		}
		all[l.Code] = m
	}
	base := all[langs[0].Code]
	for _, l := range langs[1:] {
		m := all[l.Code]
		for k := range base {
			if m[k] == "" {
				return nil, fmt.Errorf("%s.json: missing or empty key %q", l.Code, k)
			}
		}
		for k := range m {
			if _, ok := base[k]; !ok {
				return nil, fmt.Errorf("%s.json: unknown key %q", l.Code, k)
			}
		}
	}
	return all, nil
}

func render(tpl *template.Template, l Lang, s map[string]string) ([]byte, error) {
	// The strings are authored in this repository and may contain <code> and <a> markup, so they are trusted as HTML.
	t := make(map[string]template.HTML, len(s))
	for k, v := range s {
		t[k] = template.HTML(v)
	}
	ld, err := json.Marshal(map[string]any{
		"@context":            "https://schema.org",
		"@type":               "SoftwareApplication",
		"name":                "NeoSwitch",
		"description":         s["jsonld_description"],
		"inLanguage":          l.Code,
		"url":                 l.URL,
		"image":               origin + "/og.png",
		"applicationCategory": "DeveloperApplication",
		"operatingSystem":     "macOS, Linux",
		"license":             "https://github.com/yldm-ai/neo-switch/blob/main/LICENSE",
		"downloadUrl":         "https://github.com/yldm-ai/neo-switch/releases",
		"sameAs":              []string{"https://github.com/yldm-ai/neo-switch"},
		"offers":              map[string]string{"@type": "Offer", "price": "0", "priceCurrency": "USD"},
	})
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	err = tpl.Execute(&buf, page{
		Lang:    l,
		Langs:   langs,
		Default: langs[0],
		Origin:  origin,
		T:       t,
		JSONLD:  template.JS(ld),
	})
	return buf.Bytes(), err
}

// sitemap lists every language URL and, for each, the full set of hreflang alternates, as Google expects for localized pages.
func sitemap() []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">` + "\n")
	for _, l := range langs {
		fmt.Fprintf(&b, "  <url>\n    <loc>%s</loc>\n", l.URL)
		for _, a := range langs {
			fmt.Fprintf(&b, "    <xhtml:link rel=\"alternate\" hreflang=\"%s\" href=\"%s\"/>\n", a.Code, a.URL)
		}
		fmt.Fprintf(&b, "    <xhtml:link rel=\"alternate\" hreflang=\"x-default\" href=\"%s\"/>\n  </url>\n", langs[0].URL)
	}
	b.WriteString("</urlset>\n")
	return b.Bytes()
}

func copyDir(from, to string) error {
	return filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, p)
		if err != nil {
			return err
		}
		dst := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
}

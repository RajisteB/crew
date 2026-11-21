package gitx

import "testing"

func TestSlugFromURL(t *testing.T) {
	cases := []struct {
		url, slug string
		ok        bool
	}{
		{"https://github.com/acme/app", "acme/app", true},
		{"https://github.com/acme/app.git", "acme/app", true},
		{"https://github.com/acme/app/", "acme/app", true},
		{"https://github.com/acme/app.git/", "acme/app", true},
		{"https://user@github.com/acme/app.git", "acme/app", true},
		{"ssh://git@github.com/acme/app", "acme/app", true},
		{"ssh://git@github.com/acme/app.git", "acme/app", true},
		{"ssh://git@github.com:22/acme/app.git", "acme/app", true},
		{"git@github.com:acme/app", "acme/app", true},
		{"git@github.com:acme/app.git", "acme/app", true},
		{"git@github.com:acme/app.git/", "acme/app", true},
		{"/tmp/work/origin.git", "", false},
		{"../origin.git", "", false},
		{"file:///tmp/work/origin.git", "", false},
		{"git@github.com:app", "", false},
		{"https://github.com/", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		slug, ok := SlugFromURL(c.url)
		if slug != c.slug || ok != c.ok {
			t.Errorf("SlugFromURL(%q) = %q, %v; want %q, %v", c.url, slug, ok, c.slug, c.ok)
		}
	}
}

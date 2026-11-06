package useragent

import "testing"

func TestDevice(t *testing.T) {
	cases := map[string]string{
		"": "bot",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130.0 Safari/537.36":         "desktop",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148":       "mobile",
		"Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 Chrome/130.0 Mobile Safari/537.36":   "mobile",
		"Mozilla/5.0 (Linux; Android 14; SM-X710) AppleWebKit/537.36 Chrome/130.0 Safari/537.36":          "tablet",
		"Mozilla/5.0 (iPad; CPU OS 18_0 like Mac OS X) AppleWebKit/605.1.15":                              "tablet",
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)":                        "bot",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/130.0.0.0": "bot",
		"curl/8.10.1": "bot",
	}
	for ua, want := range cases {
		if got := Device(ua); got != want {
			t.Errorf("Device(%q) = %q, want %q", ua, got, want)
		}
	}
}

func TestCountry(t *testing.T) {
	cases := map[string]string{"RU": "RU", "de": "DE", "XX": "", "": "", "RUS": "", "1A": "", "xy": ""}
	for in, want := range cases {
		if got := Country(in); got != want {
			t.Errorf("Country(%q) = %q, want %q", in, got, want)
		}
	}
}

func BenchmarkDevice(b *testing.B) {
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130.0 Safari/537.36"
	for b.Loop() {
		Device(ua)
	}
}

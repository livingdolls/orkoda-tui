from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def replace(path: str, old: str, new: str, count: int = 1) -> None:
    target = ROOT / path
    content = target.read_text(encoding="utf-8")
    if old not in content:
        raise RuntimeError(f"expected snippet not found in {path}: {old[:180]!r}")
    target.write_text(content.replace(old, new, count), encoding="utf-8")


replace(
    "internal/llmprovider/service.go",
    '''\t\tif errors.Is(err, credentials.ErrNotFound) || errors.Is(err, credentials.ErrUnavailable) {
\t\t\tcredentialState[name] = false
\t\t\tcontinue
\t\t}''',
    '''\t\tif errors.Is(err, credentials.ErrNotFound) || errors.Is(err, credentials.ErrUnavailable) {
\t\t\t// A persisted TUI override must never silently fall through to an
\t\t\t// environment provider with the same name when its credential is gone.
\t\t\ts.registry.Remove(name)
\t\t\tcredentialState[name] = false
\t\t\tcontinue
\t\t}''',
)
replace(
    "internal/llmprovider/service.go",
    '''\tfor key, value := range headers {
\t\tkey = strings.TrimSpace(key)
\t\tvalue = strings.TrimSpace(value)
\t\tif key != "" && value != "" {
\t\t\tcleanHeaders[key] = value
\t\t}
\t}''',
    '''\tfor key, value := range headers {
\t\tkey = strings.TrimSpace(key)
\t\tvalue = strings.TrimSpace(value)
\t\tif sensitiveHeader(key) {
\t\t\treturn Record{}, fmt.Errorf("%w: sensitive credentials must use the API key field, not header %s", ErrInvalid, key)
\t\t}
\t\tif key != "" && value != "" {
\t\t\tcleanHeaders[key] = value
\t\t}
\t}''',
)
replace(
    "internal/llmprovider/service.go",
    '''func normalizeName(name string) string {''',
    '''func sensitiveHeader(name string) bool {
\tnormalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(name), "_", "-"), " ", "-"))
\tif normalized == "authorization" || normalized == "proxy-authorization" || normalized == "x-api-key" {
\t\treturn true
\t}
\treturn strings.Contains(normalized, "token") || strings.Contains(normalized, "secret") || strings.Contains(normalized, "api-key")
}

func normalizeName(name string) string {''',
)
replace(
    "internal/llmprovider/service_test.go",
    '''\tif err := service.Delete(ctx, "custom"); err != nil {
\t\tt.Fatal(err)
\t}''',
    '''\tif _, err := service.Save(ctx, "unsafe", SaveInput{
\t\tBaseURL: server.URL, DefaultModel: "model-a", APIKey: "provider-value",
\t\tHeaders: map[string]string{"Authorization": "Bearer should-not-enter-sqlite"},
\t}); !errors.Is(err, ErrInvalid) {
\t\tt.Fatalf("expected sensitive header rejection, got %v", err)
\t}
\tif err := service.Delete(ctx, "custom"); err != nil {
\t\tt.Fatal(err)
\t}''',
)

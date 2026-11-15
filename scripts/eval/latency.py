import http.cookiejar, json, statistics, time, urllib.request
B = "http://127.0.0.1:18080"
def session(login):
    jar = http.cookiejar.CookieJar()
    op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
    def call(method, path, body=None):
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(B + path, data=data, method=method, headers={"Content-Type": "application/json", "X-Requested-With": "tropa"})
        t = time.perf_counter(); r = op.open(req); r.read(); return (time.perf_counter() - t) * 1000
    call("POST", "/api/auth/login", {"login": login, "password": "buddy2026"})
    return call
alex, dana, artem, maria = session("alex"), session("dana"), session("artem"), session("maria")
alex("POST", "/api/demo/reset", {})
cases = [
    ("GET /api/route — маршрут стажёра", alex, "GET", "/api/route", None, 40),
    ("GET /api/actions — заявки и квитанции", alex, "GET", "/api/actions", None, 40),
    ("Чат: «Как проходит код-ревью?» (поиск по базе)", maria, "POST", "/api/chat", {"text": "Как проходит код-ревью?"}, 25),
    ("Чат: «Что мне делать дальше?» (маршрут)", dana, "POST", "/api/chat", {"text": "Что мне делать дальше?"}, 25),
    ("Чат: «Оформи отгул на 18 ноября» (план + MCP-календарь)", artem, "POST", "/api/chat", {"text": "Оформи отгул на 18 ноября"}, 25),
]
print("| Операция | p50, мс | p95, мс | max, мс |\n|---|---|---|---|")
for name, call, m, p, b, n in cases:
    xs = sorted(call(m, p, b) for _ in range(n))
    print(f"| {name} | {statistics.median(xs):.1f} | {xs[max(0,int(len(xs)*0.95)-1)]:.1f} | {xs[-1]:.1f} |")

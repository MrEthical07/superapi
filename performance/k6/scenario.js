import http from "k6/http";
import { check } from "k6";
import exec from "k6/execution";
import { Counter, Gauge, Rate, Trend } from "k6/metrics";
import encoding from "k6/encoding";

const BASE_URL = (__ENV.BASE_URL || "http://127.0.0.1:8080").replace(/\/$/, "");
const AUTH_IDENTIFIER = (__ENV.AUTH_IDENTIFIER || "").trim();
const AUTH_PASSWORD = __ENV.AUTH_PASSWORD || "";
const AUTH_IDENTIFIER_PREFIX = (__ENV.AUTH_IDENTIFIER_PREFIX || "").trim();
const AUTH_IDENTIFIER_DOMAIN = (__ENV.AUTH_IDENTIFIER_DOMAIN || "example.com").trim();
const AUTH_USER_POOL_SIZE = Number(__ENV.AUTH_USER_POOL_SIZE || "1");
const AUTH_LOGIN_PATH = (__ENV.AUTH_LOGIN_PATH || "/api/v1/system/auth/login").trim();
const AUTH_REFRESH_PATH = (__ENV.AUTH_REFRESH_PATH || "/api/v1/system/auth/refresh").trim();
const REQUEST_TIMEOUT = (__ENV.REQUEST_TIMEOUT || "5s").trim();

const TARGET_RPS = Number(__ENV.TARGET_RPS || "10000");
const START_RPS = Number(__ENV.START_RPS || "500");
const RAMP_DURATION = (__ENV.RAMP_DURATION || "5m").trim();
const SUSTAIN_DURATION = (__ENV.SUSTAIN_DURATION || "10m").trim();
const RAMP_STEPS = Number(__ENV.RAMP_STEPS || "10");
const PRE_ALLOCATED_VUS = Number(__ENV.PREALLOCATED_VUS || "2000");
const MAX_VUS = Number(__ENV.MAX_VUS || "30000");

const ERROR_THRESHOLD = Number(__ENV.ERROR_THRESHOLD || "0.01");
const AUTH_401_RATE_THRESHOLD = Number(__ENV.AUTH_401_RATE_THRESHOLD || "0.01");
const SUSTAIN_RPS_RATIO = Number(__ENV.SUSTAIN_RPS_RATIO || "0.95");
const P50_THRESHOLD_MS = Number(__ENV.P50_THRESHOLD_MS || "50");
const P95_THRESHOLD_MS = Number(__ENV.P95_THRESHOLD_MS || "100");
const P99_THRESHOLD_MS = Number(__ENV.P99_THRESHOLD_MS || "250");
const REFRESH_BUFFER_SECONDS = Number(__ENV.REFRESH_BUFFER_SECONDS || "30");

if (!AUTH_IDENTIFIER && !AUTH_IDENTIFIER_PREFIX) {
  throw new Error("Set AUTH_IDENTIFIER or AUTH_IDENTIFIER_PREFIX for per-VU login.");
}
if (!AUTH_PASSWORD) {
  throw new Error("AUTH_PASSWORD is required for per-VU login.");
}
if (TARGET_RPS <= 0 || START_RPS <= 0) {
  throw new Error("TARGET_RPS and START_RPS must be greater than zero.");
}

const ROUTES = [
  {
    name: "healthz",
    weight: 35,
    method: "GET",
    path: "/healthz",
    body: null,
    headers: {},
  },
  {
    name: "readyz",
    weight: 25,
    method: "GET",
    path: "/readyz",
    body: null,
    headers: {},
  },
  {
    name: "parse_duration",
    weight: 20,
    method: "POST",
    path: "/system/parse-duration",
    body: JSON.stringify({ duration: "250ms" }),
    headers: { "Content-Type": "application/json" },
  },
  {
    name: "whoami",
    weight: 20,
    method: "GET",
    path: "/api/v1/system/whoami",
    body: null,
    headers: {},
  },
];

const TOTAL_WEIGHT = ROUTES.reduce((sum, route) => sum + route.weight, 0);
const expectedStatus200 = http.expectedStatuses(200);

const success200Total = new Counter("success_200_total");
const fail401Total = new Counter("fail_401_total");
const fail5xxTotal = new Counter("fail_5xx_total");
const failTimeoutTotal = new Counter("fail_timeout_total");
const failOtherTotal = new Counter("fail_other_total");
const loginAttemptTotal = new Counter("auth_login_attempt_total");
const loginFailureTotal = new Counter("auth_login_failure_total");
const refreshAttemptTotal = new Counter("auth_refresh_attempt_total");
const refreshFailureTotal = new Counter("auth_refresh_failure_total");
const retryAfter401Total = new Counter("retry_after_401_total");
const authLifecycleFailureTotal = new Counter("auth_lifecycle_failure_total");
const status401Rate = new Rate("status_401_rate");
const successfulReqDuration = new Trend("successful_req_duration", true);
const scenarioVUs = new Gauge("scenario_vus");

const latencyLE1msTotal = new Counter("latency_le_1ms_total");
const latencyLE2msTotal = new Counter("latency_le_2ms_total");
const latencyLE5msTotal = new Counter("latency_le_5ms_total");
const latencyLE10msTotal = new Counter("latency_le_10ms_total");
const latencyLE20msTotal = new Counter("latency_le_20ms_total");
const latencyLE50msTotal = new Counter("latency_le_50ms_total");
const latencyLE100msTotal = new Counter("latency_le_100ms_total");
const latencyLE250msTotal = new Counter("latency_le_250ms_total");
const latencyGT250msTotal = new Counter("latency_gt_250ms_total");

const rampDurationSeconds = parseDurationSeconds(RAMP_DURATION, "RAMP_DURATION");
const sustainDurationSeconds = parseDurationSeconds(SUSTAIN_DURATION, "SUSTAIN_DURATION");
const scenarioNames = [];
const scenarios = buildScenarios();
const thresholds = buildThresholds();

export const options = {
  discardResponseBodies: true,
  scenarios,
  thresholds,
};

const vuAuthState = {
  accessToken: "",
  refreshToken: "",
  accessExpiryUnix: 0,
  identifier: "",
};

function parseDurationSeconds(raw, envName) {
  const value = String(raw || "").trim().toLowerCase();
  const match = value.match(/^(\d+)(ms|s|m|h)$/);
  if (!match) {
    throw new Error(`${envName} must be in the form <number><unit>, for example 30s, 5m, or 1h.`);
  }

  const amount = Number(match[1]);
  const unit = match[2];
  if (!Number.isFinite(amount) || amount < 0) {
    throw new Error(`${envName} must be a non-negative number.`);
  }

  switch (unit) {
    case "ms":
      return Math.max(1, Math.ceil(amount / 1000));
    case "s":
      return amount;
    case "m":
      return amount * 60;
    case "h":
      return amount * 3600;
    default:
      throw new Error(`${envName} has unsupported unit ${unit}.`);
  }
}

function buildScenarios() {
  const built = {};
  let offsetSeconds = 0;

  if (rampDurationSeconds > 0) {
    const steps = Math.max(1, Math.min(Math.floor(RAMP_STEPS), rampDurationSeconds));
    const baseDuration = Math.floor(rampDurationSeconds / steps);
    const remainder = rampDurationSeconds % steps;

    for (let i = 1; i <= steps; i += 1) {
      const scenarioName = `ramp_${String(i).padStart(2, "0")}`;
      const durationSeconds = baseDuration + (i <= remainder ? 1 : 0);
      const progress = i / steps;
      const rate = Math.max(1, Math.round(START_RPS + (TARGET_RPS - START_RPS) * progress));

      built[scenarioName] = {
        executor: "constant-arrival-rate",
        exec: "runTraffic",
        rate,
        timeUnit: "1s",
        duration: `${durationSeconds}s`,
        startTime: `${offsetSeconds}s`,
        preAllocatedVUs: PRE_ALLOCATED_VUS,
        maxVUs: MAX_VUS,
        gracefulStop: "30s",
      };

      scenarioNames.push(scenarioName);
      offsetSeconds += durationSeconds;
    }
  }

  built.sustain_10k = {
    executor: "constant-arrival-rate",
    exec: "runTraffic",
    rate: TARGET_RPS,
    timeUnit: "1s",
    duration: `${sustainDurationSeconds}s`,
    startTime: `${offsetSeconds}s`,
    preAllocatedVUs: PRE_ALLOCATED_VUS,
    maxVUs: MAX_VUS,
    gracefulStop: "30s",
  };
  scenarioNames.push("sustain_10k");

  return built;
}

function buildThresholds() {
  const built = {
    http_req_failed: [`rate<${ERROR_THRESHOLD}`],
    dropped_iterations: ["count==0"],
    successful_req_duration: [`p(50)<${P50_THRESHOLD_MS}`, `p(95)<${P95_THRESHOLD_MS}`, `p(99)<${P99_THRESHOLD_MS}`],
    checks: ["rate>0.99"],
    status_401_rate: [`rate<=${AUTH_401_RATE_THRESHOLD}`],
    auth_lifecycle_failure_total: ["count==0"],
  };

  for (const route of ROUTES) {
    built[`status_401_rate{route:${route.name}}`] = [`rate<=${AUTH_401_RATE_THRESHOLD}`];
  }

  for (const scenarioName of scenarioNames) {
    const rpsThreshold = scenarioName === "sustain_10k" ? `rate>=${Math.floor(TARGET_RPS * SUSTAIN_RPS_RATIO)}` : "rate>=0";
    built[`http_reqs{scenario:${scenarioName}}`] = [rpsThreshold];
    built[`scenario_vus{scenario:${scenarioName}}`] = ["value>=0"];
  }

  return built;
}

function pickRoute() {
  let draw = Math.random() * TOTAL_WEIGHT;
  for (const route of ROUTES) {
    draw -= route.weight;
    if (draw <= 0) {
      return route;
    }
  }
  return ROUTES[ROUTES.length - 1];
}

function nowUnix() {
  return Math.floor(Date.now() / 1000);
}

function identifierForCurrentVU() {
  if (AUTH_USER_POOL_SIZE <= 1) {
    return AUTH_IDENTIFIER;
  }

  const vuID = exec.vu.idInTest;
  const poolIndex = ((vuID - 1) % AUTH_USER_POOL_SIZE) + 1;

  let localPart = AUTH_IDENTIFIER_PREFIX;
  let domain = AUTH_IDENTIFIER_DOMAIN;

  if (!localPart && AUTH_IDENTIFIER.includes("@")) {
    const parts = AUTH_IDENTIFIER.split("@");
    localPart = parts[0];
    if (parts.length > 1 && parts[1]) {
      domain = parts[1];
    }
  }
  if (!localPart) {
    localPart = AUTH_IDENTIFIER || "loadtest";
  }
  if (!domain) {
    domain = "example.com";
  }

  return `${localPart}+vu${poolIndex}@${domain}`;
}

function decodeTokenExpiryUnix(accessToken) {
  const parts = String(accessToken || "").split(".");
  if (parts.length < 2) {
    return 0;
  }

  try {
    const payloadRaw = encoding.b64decode(parts[1], "rawurl", "s");
    const payload = JSON.parse(payloadRaw);
    const exp = Number(payload.exp || 0);
    if (!Number.isFinite(exp) || exp <= 0) {
      return 0;
    }
    return Math.floor(exp);
  } catch (_error) {
    return 0;
  }
}

function parseTokenEnvelope(response) {
  let body;
  try {
    body = response.json();
  } catch (_error) {
    return null;
  }

  if (!body || body.ok !== true || !body.data) {
    return null;
  }

  const accessToken = String(body.data.access_token || "").trim();
  const refreshToken = String(body.data.refresh_token || "").trim();
  if (!accessToken || !refreshToken) {
    return null;
  }

  let accessExpiryUnix = Number(body.data.access_expires_unix || 0);
  if (!Number.isFinite(accessExpiryUnix) || accessExpiryUnix <= 0) {
    accessExpiryUnix = decodeTokenExpiryUnix(accessToken);
  }
  if (!Number.isFinite(accessExpiryUnix) || accessExpiryUnix <= 0) {
    return null;
  }

  return {
    accessToken,
    refreshToken,
    accessExpiryUnix: Math.floor(accessExpiryUnix),
  };
}

function applyTokenBundle(bundle) {
  vuAuthState.accessToken = bundle.accessToken;
  vuAuthState.refreshToken = bundle.refreshToken;
  vuAuthState.accessExpiryUnix = bundle.accessExpiryUnix;
}

function loginForVU() {
  loginAttemptTotal.add(1);

  const identifier = identifierForCurrentVU();
  if (!identifier) {
    loginFailureTotal.add(1, { status: "identifier_missing" });
    return false;
  }

  const response = http.post(
    `${BASE_URL}${AUTH_LOGIN_PATH}`,
    JSON.stringify({ identifier, password: AUTH_PASSWORD }),
    {
      headers: { "Content-Type": "application/json" },
      tags: { route: "auth_login" },
      responseCallback: expectedStatus200,
      timeout: REQUEST_TIMEOUT,
      responseType: "text",
    },
  );

  classifyResponse("auth_login", response);

  if (response.status !== 200) {
    loginFailureTotal.add(1, { status: String(response.status) });
    return false;
  }

  const tokenBundle = parseTokenEnvelope(response);
  if (!tokenBundle) {
    loginFailureTotal.add(1, { status: "invalid_payload" });
    return false;
  }

  applyTokenBundle(tokenBundle);
  vuAuthState.identifier = identifier;
  return true;
}

function refreshForVU() {
  if (!vuAuthState.refreshToken) {
    return false;
  }

  refreshAttemptTotal.add(1);
  const response = http.post(
    `${BASE_URL}${AUTH_REFRESH_PATH}`,
    JSON.stringify({ refresh_token: vuAuthState.refreshToken }),
    {
      headers: { "Content-Type": "application/json" },
      tags: { route: "auth_refresh" },
      responseCallback: expectedStatus200,
      timeout: REQUEST_TIMEOUT,
      responseType: "text",
    },
  );

  classifyResponse("auth_refresh", response);

  if (response.status !== 200) {
    refreshFailureTotal.add(1, { status: String(response.status) });
    return false;
  }

  const tokenBundle = parseTokenEnvelope(response);
  if (!tokenBundle) {
    refreshFailureTotal.add(1, { status: "invalid_payload" });
    return false;
  }

  applyTokenBundle(tokenBundle);
  return true;
}

function ensureAuthReady(forceRefresh) {
  if (!vuAuthState.accessToken || !vuAuthState.refreshToken || vuAuthState.accessExpiryUnix <= 0) {
    return loginForVU();
  }

  if (forceRefresh || nowUnix() + REFRESH_BUFFER_SECONDS >= vuAuthState.accessExpiryUnix) {
    if (refreshForVU()) {
      return true;
    }
    return loginForVU();
  }

  return true;
}

function requestRoute(route) {
  const headers = Object.assign({}, route.headers, {
    Authorization: `Bearer ${vuAuthState.accessToken}`,
  });

  return http.request(route.method, `${BASE_URL}${route.path}`, route.body, {
    headers,
    tags: { route: route.name },
    responseCallback: expectedStatus200,
    timeout: REQUEST_TIMEOUT,
  });
}

function recordLatencyHistogram(durationMs, routeName) {
  const tags = { route: routeName };

  if (durationMs <= 1) {
    latencyLE1msTotal.add(1, tags);
  } else if (durationMs <= 2) {
    latencyLE2msTotal.add(1, tags);
  } else if (durationMs <= 5) {
    latencyLE5msTotal.add(1, tags);
  } else if (durationMs <= 10) {
    latencyLE10msTotal.add(1, tags);
  } else if (durationMs <= 20) {
    latencyLE20msTotal.add(1, tags);
  } else if (durationMs <= 50) {
    latencyLE50msTotal.add(1, tags);
  } else if (durationMs <= 100) {
    latencyLE100msTotal.add(1, tags);
  } else if (durationMs <= 250) {
    latencyLE250msTotal.add(1, tags);
  } else {
    latencyGT250msTotal.add(1, tags);
  }
}

function classifyResponse(routeName, response) {
  status401Rate.add(response.status === 401 ? 1 : 0, { route: routeName });

  if (response.status === 200) {
    success200Total.add(1, { route: routeName });
    successfulReqDuration.add(response.timings.duration, { route: routeName });
    recordLatencyHistogram(response.timings.duration, routeName);
    return;
  }

  if (response.status === 401) {
    fail401Total.add(1, { route: routeName });
    return;
  }

  if (response.status >= 500 && response.status <= 599) {
    fail5xxTotal.add(1, { route: routeName, status: String(response.status) });
    return;
  }

  if (response.status === 0) {
    failTimeoutTotal.add(1, { route: routeName });
    return;
  }

  failOtherTotal.add(1, { route: routeName, status: String(response.status) });
}

export function runTraffic() {
  scenarioVUs.add(exec.instance.vusActive, { scenario: exec.scenario.name });

  if (!ensureAuthReady(false)) {
    authLifecycleFailureTotal.add(1, { scenario: exec.scenario.name });
    return;
  }

  const route = pickRoute();
  let response = requestRoute(route);
  classifyResponse(route.name, response);

  if (response.status === 401) {
    retryAfter401Total.add(1, { route: route.name });
    if (ensureAuthReady(true)) {
      response = requestRoute(route);
      classifyResponse(route.name, response);
    }
  }

  check(response, {
    [`${route.name} status is 200`]: (r) => r.status === 200,
  });
}

export default runTraffic;

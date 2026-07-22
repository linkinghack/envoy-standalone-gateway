import {expect, test, type Page} from "@playwright/test";

test("restores a session and renders the operational overview", async ({page}) => {
  await mockAuthenticatedGateway(page);
  await page.goto("/");
  await expect(page.getByRole("heading", {name: "The edge, at a glance."})).toBeVisible();
  await expect(page.getByText("Ready", {exact: true}).first()).toBeVisible();
  await expect(page.getByText("1.39.0")).toBeVisible();
  if (process.env.ESGW_SCREENSHOT) {
    await page.screenshot({path: process.env.ESGW_SCREENSHOT, fullPage: true});
  }
});

test("mobile navigation preserves every primary destination", async ({page}, testInfo) => {
  test.skip(testInfo.project.name !== "mobile", "mobile-only assertion");
  await mockAuthenticatedGateway(page);
  await page.goto("/");
  await page.getByRole("button", {name: "Open navigation"}).click();
  await expect(page.getByRole("link", {name: /Configuration/})).toBeVisible();
  await page.getByRole("link", {name: /Runtime/}).click();
  await expect(page.getByRole("heading", {name: "What Envoy sees."})).toBeVisible();
});

test("fresh gateways show the one-time bootstrap surface", async ({page}) => {
  await page.route("**/api/v1/auth/bootstrap", async (route) => {
    await route.fulfill({status: 200, contentType: "application/json", body: JSON.stringify({required: true, expiresAt: "2026-07-22T12:30:00Z"})});
  });
  await page.goto("/");
  await expect(page.getByRole("heading", {name: "Claim this gateway"})).toBeVisible();
  await expect(page.getByLabel(/New password/)).toBeVisible();
});

async function mockAuthenticatedGateway(page: Page) {
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    const payloads: Record<string, unknown> = {
      "/api/v1/auth/bootstrap": {required: false, expiresAt: null},
      "/api/v1/auth/session": {user: {name: "admin", roles: ["admin"]}, expiresAt: "2026-07-23T12:00:00Z"},
      "/api/v1/status/summary": {nodeId: "esgw-node", ready: true, readyStatus: "LIVE", stale: false, collectedAt: "2026-07-22T12:00:00Z", envoy: {version: "1.39.0", state: "LIVE"}, counts: {listeners: 2, clusters: 3, endpoints: 4, routes: 1, certs: 1}},
      "/api/v1/config/status": {delivery: {mode: "xds", state: "effective"}},
      "/api/v1/status/listeners": {items: [{name: "web", address: "0.0.0.0:8080"}], stale: false},
      "/api/v1/status/clusters": {items: [{name: "backend", endpoints: []}], stale: false},
      "/api/v1/status/routes": {items: [{name: "main", virtualHosts: []}], stale: false},
      "/api/v1/status/certs": {items: [], stale: false},
    };
    await route.fulfill({status: 200, contentType: "application/json", body: JSON.stringify(payloads[path] ?? {})});
  });
}

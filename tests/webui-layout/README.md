# Route operation controls and notifications

Run `node tests/webui-layout/server.mjs --port=28411` from any directory, then open
`http://127.0.0.1:28411/layout?mode=paused&theme=light` in a real browser.
The server binds only to loopback. It serves the current application HTML, CSS,
and notification/operation/dialog scripts; only the external operation-status
boundary is a deterministic fixture. No router is contacted.

Await `layoutProbe.ready`, then inspect `layoutProbe.capture()`. A passing case
has a visible toast, no intersection with the operation widget, and every visible
operation control receives pointer hits. This catches notifications covering the
real Stop/Resume/minimize/open controls. It also requires notifications to remain
inside the viewport and to return to the lower right when the operation ends.

Cases: desktop 1264x731 and a narrow viewport; light/dark; paused/running;
three notifications; minimize/reopen; resize; and inactive via
`await layoutProbe.setMode('inactive')`. Use native browser clicks to check that
Resume emits the actual `broray:routes-operation-resume` event. Its consumer is
intercepted by this test utility so that it cannot start any operation.

Always record `innerWidth`/`innerHeight`: Firefox window controls may report the
requested outer dimensions while enforcing a larger minimum content width.
The fixture explicitly enters the loaded, authenticated view; authentication is
outside this component test. A hidden operation widget is a failing fixture,
not evidence that its controls are unobscured.
For an exact small layout when the window enforces a 500px minimum, use
`await layoutProbe.frameViewport(390, 700, 'mode=paused&theme=dark&stack=3')`.
This runs the same page/assets in a real nested browsing viewport; label its
results separately from a top-level window or physical phone.

Close the fixture with `POST /__fixture__/shutdown`. Read `/__fixture__/info`
first to confirm its fixture ID; never stop another process merely by port/PID.


Logo contrast: evaluate logo-contrast.probe.js in the local fixture, then run runLogoContrast with broray, day and night. Each scene renders actual card logo CSS and SVG in light/dark embedding schemes. Capture actual browser screenshots and count non-white glyph pixels in the returned imageRect (or inspect visually). Do not use canvas drawImage as a paint oracle: Firefox rerasterizes this SVG using a different color scheme. Real theme tokens are broray/day/night; light/dark query aliases map to day/night.

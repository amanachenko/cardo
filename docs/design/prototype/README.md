# Launcher comp

Open `quickstart.html` in a browser. It is one self-contained file on purpose: it can be sent to
someone without a build step.

The comp predates two decisions and is kept as the record of the design, not as the interface: its
copy is Russian and its buttons are simulated. The shipped board is
`internal/launcher/web/index.html`, in English, driven by the engine.

- `decision.html` — the three surface structures that were dealt, kept as the record of the choice.
- `PRODUCT.md` — product truth captured before design (Impeccable `init`).
- `.impeccable/config.json` — build path and seed, so the composition can be reproduced.

The stylesheet that production uses is [`../cardo-ui.css`](../cardo-ui.css); the comp predates it and
repeats the values. When this becomes real code it imports the stylesheet and this directory is
archived. Rules and tokens: [`../DESIGN.md`](../DESIGN.md). Flow and checks:
[`../quickstart-launcher.md`](../quickstart-launcher.md).

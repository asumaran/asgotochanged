# shellcheck shell=bash
# scenario.sh — demo session for the README GIF, run by `asdemo record`
# (asumaran/asdemokit). Sourced by the kit; the helpers used below
# (demo_*) come from it.

DEMO_SESSION="asgotochangeddemo"
DEMO_OUT="docs/demo.gif"

# asgotochanged lists what the branch of the focused pane changed. No personal
# checkout has a branch worth showing (the shopnest worktrees change one file
# each), so the demo works on a throwaway clone of shopnest with a scripted
# feature branch: committed, pending, deleted and untracked files. Nothing in
# the real repository is touched.
DEMO_SOURCE_REPO="$HOME/Developer/shopnest"
DEMO_CLONE="$HOME/.cache/asgotochanged-demo/shopnest"
DEMO_START_CWD="$DEMO_CLONE"
# Enter opens the editor on camera: a bare nvim, without the user's plugins.
DEMO_SESSION_ENV=("ASGOTOCHANGED_EDITOR=nvim --clean")

# Same sidebar as the other demos: personal repos only.
REPOS=(
  "$HOME/Developer/worktree-cli"
  "$HOME/Developer/asreviewer"
  "$HOME/Developer/aspage"
)

demo_build() {
  local version
  version="$(sed -n 's/^version = "\(.*\)"/\1/p' herdr-plugin.toml)"
  go build -ldflags "-X main.version=v${version}" -o asgotochanged .

  rm -rf "$DEMO_CLONE"
  mkdir -p "$(dirname "$DEMO_CLONE")"
  git clone -q "$DEMO_SOURCE_REPO" "$DEMO_CLONE"
  (
    cd "$DEMO_CLONE"
    g() { git -c user.name=demo -c user.email=demo@example.com -c commit.gpgsign=false "$@"; }
    # the clone's origin/HEAD follows whatever the source has checked out
    g remote set-head origin main
    g checkout -q -b feat/cart-tax origin/main

    cat > src/lib/tax.ts <<'TS'
import { CartItem } from "@/types";

export const TAX_RATE = 0.21;

// Tax is charged on the subtotal of every line, rounded per line so the
// total matches what the customer sees next to each item.
export function calculateTax(items: CartItem[]): number {
  return items.reduce(
    (total, item) => total + Math.round(item.subtotal * TAX_RATE * 100) / 100,
    0
  );
}
TS
    cat >> src/lib/cart-utils.ts <<'TS'

export function calculateCartTotalWithTax(items: CartItem[]): number {
  const subtotal = items.reduce((total, item) => total + item.subtotal, 0);
  return subtotal + calculateTax(items);
}
TS
    sed -i '' '1s|^|import { calculateTax } from "@/lib/tax";\n|' src/lib/cart-utils.ts
    g rm -q .eslintrc.json
    printf 'import next from "eslint-config-next";\n\nexport default [...next];\n' > eslint.config.mjs
    g add -A
    g commit -q -m "feat(cart): charge tax on the cart total"

    # pending and untracked work on top of the commit
    printf '\n// TODO(cart-tax): show the tax line under the subtotal\n' >> src/components/CartSummary.tsx
    mkdir -p notes
    printf '# Cart tax\n\n- [x] tax per line, rounded\n- [ ] tax line in the summary\n- [ ] tests for the rounding\n' > notes/PLAN.md
  )
}

demo_teardown() {
  go build -o asgotochanged . 2>/dev/null || true
  rm -rf "$HOME/.cache/asgotochanged-demo"
}

demo_setup() {
  local repo
  demo_adopt_repo "$(demo_first_workspace)" "$DEMO_START_CWD"
  for repo in "${REPOS[@]}"; do demo_open_repo "$repo" >/dev/null; done
  demo_split_below "$DEMO_START_CWD" >/dev/null
}

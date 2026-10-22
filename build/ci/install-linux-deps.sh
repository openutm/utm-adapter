#!/usr/bin/env bash
set -euo pipefail

: "${RUNNER_TEMP:?This script is intended for GitHub Actions runners}"

# Keep the build's Ubuntu sources separate from runner and third-party sources.
# The runner's HTTP Azure mirror downloaded at only 121 kB/s in our baseline.
apt_dir="$RUNNER_TEMP/utm-adapter-apt"
mkdir -p "$apt_dir/archives"
cat > "$apt_dir/ubuntu.sources" <<'EOF'
Types: deb
URIs: https://archive.ubuntu.com/ubuntu/
Suites: noble noble-updates
Components: main universe
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg

Types: deb
URIs: https://security.ubuntu.com/ubuntu/
Suites: noble-security
Components: main universe
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg
EOF

apt_options=(
  -o "Dir::Etc::sourcelist=$apt_dir/ubuntu.sources"
  -o "Dir::Etc::sourceparts=-"
  -o "Dir::Cache::archives=$apt_dir/archives"
  -o "APT::Keep-Downloaded-Packages=true"
)

# Always refresh indexes and let APT validate cached archives against them.
sudo apt-get "${apt_options[@]}" update
sudo apt-get "${apt_options[@]}" install -y --no-install-recommends \
  build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev libx11-dev

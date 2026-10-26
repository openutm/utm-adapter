#!/usr/bin/env bash
# Runs the adapter's Go HTTP client against the actual FastAPI/SQLAlchemy app.
set -euo pipefail
adapter_root="$(cd "$(dirname "$0")/.." && pwd)"
blender_root="${FLIGHT_BLENDER_ROOT:-$(dirname "$adapter_root")/flight-blender}"
python_bin="${FLIGHT_BLENDER_PYTHON:-$blender_root/.venv/bin/python}"
test_dir="$(mktemp -d)"
trap 'if [[ -n "${server_pid:-}" ]]; then kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; fi; rm -rf "$test_dir"' EXIT
export DATABASE_URL="sqlite:///$test_dir/blender.sqlite"
export BYPASS_AUTH_TOKEN_VERIFICATION=True
export PYTHONPATH="$blender_root/src"
export UTM_TEST_FLIGHT_BLENDER_TOKEN
UTM_TEST_FLIGHT_BLENDER_TOKEN="$($python_bin -c 'import jwt; print(jwt.encode({"sub":"adapter-contract-test","iss":"https://integration.example.test","aud":"testflight.flightblender.com","scope":"flightblender.read flightblender.write"}, "integration-test-key-32-characters", algorithm="HS256"))')"
# Let the OS select a port. Keep the listening socket open until uvicorn owns it.
"$python_bin" - "$test_dir/port" > "$test_dir/server.log" 2>&1 <<'PY' &
import socket
import sys
import uvicorn
from flight_blender.api.main import create_fastapi_app
from flight_blender.db.session import Base, engine
from flight_blender import models
from sqlalchemy.ext.compiler import compiles
from sqlalchemy.dialects.postgresql import UUID as PGUUID
@compiles(PGUUID, "sqlite")
def sqlite_uuid(type_, compiler, **kw):
    return "CHAR(32)"
Base.metadata.create_all(engine)
from datetime import datetime, timezone
from uuid import UUID
from sqlalchemy.orm import Session
from flight_blender.models.flight_declarations_orm import FlightDeclarationORM
with Session(engine) as db:
    db.add(FlightDeclarationORM(id=UUID("00000000-0000-0000-0000-000000000001"), aircraft_id="test", operational_intent="{}", bounds="0,0,1,1", state=5,
        start_datetime=datetime(2020,1,1,tzinfo=timezone.utc), end_datetime=datetime(2020,1,2,tzinfo=timezone.utc)))
    db.commit()
sock = socket.socket()
sock.bind(('127.0.0.1', 0))
sock.listen(128)
with open(sys.argv[1], 'w') as port_file:
    port_file.write(str(sock.getsockname()[1]))
uvicorn.Server(uvicorn.Config(create_fastapi_app(), log_level='warning')).run(sockets=[sock])
PY
server_pid=$!
for attempt in {1..100}; do
 if [[ -s "$test_dir/port" ]]; then
  export UTM_TEST_FLIGHT_BLENDER_URL="http://127.0.0.1:$(cat "$test_dir/port")"
  if curl -fsS "$UTM_TEST_FLIGHT_BLENDER_URL/ping/" > /dev/null 2>&1; then break; fi
 fi
 if ! kill -0 "$server_pid" 2>/dev/null; then cat "$test_dir/server.log"; exit 1; fi
 sleep 0.1
done
cd "$adapter_root"
if ! go test -race ./internal/adapter -run '^TestFlightBlender(SessionContract|Workflow)$' -count=1 -v; then cat "$test_dir/server.log"; exit 1; fi
# Optional second client: exercise the Android JVM core against this same API/database.
if [[ -n "${DJI_ANDROID_ROOT:-}" ]]; then
 (cd "$DJI_ANDROID_ROOT" && ./gradlew :core:test --tests '*ArchiveContractIntegrationTest' --rerun-tasks --console=plain) || { cat "$test_dir/server.log"; exit 1; }
fi

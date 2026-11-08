# shellcheck shell=bash
# The translation GPU instance's whole life, run once by cloud-init as root.
# translate-gpu prepends `#!/bin/bash` and the profile's fields as upper-cased
# shell variables; this reads IMAGE, MODEL, REVISION, SERVED_NAME, SERVER_ARGS
# (an array), MAX_LIFETIME_MIN, IDLE_MIN and BOOT_DEADLINE_MIN.
set -Eeuo pipefail

# How the instance ends, whatever ends it: a power-off that skips the orderly
# shutdown — nothing here needs one, and it takes minutes — which the launch
# makes a termination.
cat >/usr/local/sbin/translate-gpu-end <<'EOF'
#!/bin/sh
exec systemctl poweroff --force
EOF
chmod +x /usr/local/sbin/translate-gpu-end

# Every line the instance's own work writes goes to the serial console, which
# EC2's console output reads back, tagged `translate-gpu| ` so that
# translate-gpu (consoleTag) can pick it out from the kernel's and cloud-init's.
cat >/usr/local/sbin/translate-gpu-console <<'EOF'
#!/bin/sh
exec sed -u 's/^/translate-gpu| /' >/dev/console
EOF
chmod +x /usr/local/sbin/translate-gpu-console
exec > >(/usr/local/sbin/translate-gpu-console) 2>&1
trap 'echo "failed at line $LINENO: $BASH_COMMAND"; /usr/local/sbin/translate-gpu-end' ERR

# The ceiling, whatever else happens.
systemd-run --unit=translate-gpu-lifetime --on-active="${MAX_LIFETIME_MIN}m" /usr/local/sbin/translate-gpu-end

# The watchdog starts first, so the boot deadline holds whatever follows.
cat >/usr/local/sbin/translate-gpu-watchdog <<'EOF'
#!/bin/bash
# Ends the instance when vLLM is not serving by the boot deadline, or once it
# has served nothing for the idle limit: nothing running, nothing waiting, and
# no request finished since the last look. A server that stops answering
# counts as idle.
exec > >(/usr/local/sbin/translate-gpu-console) 2>&1
url=http://127.0.0.1:8000
until curl -sf -o /dev/null "$url/health"; do
  if ((SECONDS >= BOOT_DEADLINE_MIN * 60)); then
    echo "not serving after $BOOT_DEADLINE_MIN min"
    exec /usr/local/sbin/translate-gpu-end
  fi
  sleep 15
done
echo "serving"
finished=0 active=$SECONDS
while sleep 60; do
  if metrics=$(curl -sf "$url/metrics"); then
    read -r busy seen < <(awk '
      /^vllm:num_requests_(running|waiting)[{ ]/ { b += $NF }
      /^vllm:request_success_total[{ ]/ { f += $NF }
      END { printf "%d %d\n", b, f }' <<<"$metrics")
    if ((busy > 0 || seen != finished)); then
      finished=$seen active=$SECONDS
    fi
  fi
  if ((SECONDS - active >= IDLE_MIN * 60)); then
    echo "idle for $IDLE_MIN min"
    exec /usr/local/sbin/translate-gpu-end
  fi
done
EOF
chmod +x /usr/local/sbin/translate-gpu-watchdog
systemd-run --unit=translate-gpu-watchdog \
  --setenv=BOOT_DEADLINE_MIN="$BOOT_DEADLINE_MIN" --setenv=IDLE_MIN="$IDLE_MIN" \
  /usr/local/sbin/translate-gpu-watchdog
echo "watchdog armed: serving within $BOOT_DEADLINE_MIN min, idle limit $IDLE_MIN min, lifetime $MAX_LIFETIME_MIN min"

# Image layers and weights live on the instance-store NVMe: faster than the
# root volume, and gone with the instance. Both of Docker's stores move,
# whichever of them holds images on this build.
nvme=/opt/dlami/nvme
mountpoint -q "$nvme"
systemctl stop docker.socket docker.service containerd.service
for store in docker containerd; do
  mkdir -p "$nvme/$store"
  mount --bind "$nvme/$store" "/var/lib/$store"
done
systemctl start containerd.service docker.service
echo "docker stores on the instance-store NVMe"

echo "pulling $IMAGE"
docker pull --quiet "$IMAGE"
echo "starting vLLM: it downloads $MODEL at $REVISION, then loads it"
docker run -d --name vllm --gpus all --ipc=host -p 127.0.0.1:8000:8000 \
  -v "$nvme/hf:/root/.cache/huggingface" \
  "$IMAGE" "$MODEL" --revision "$REVISION" --tokenizer-revision "$REVISION" \
  --served-model-name "$SERVED_NAME" "${SERVER_ARGS[@]}"
# vLLM's own log — the download, the load, the startup — on the console too.
systemd-run --unit=translate-gpu-vllm-log /bin/bash -c 'docker logs --follow vllm 2>&1 | /usr/local/sbin/translate-gpu-console'

#!/bin/bash
#
# Flash Raspberry Pi OS to every EXTERNAL disk attached to this (macOS)
# machine, in parallel, then pre-seed ssh + user credentials on the boot
# partitions so the Pis come up ready for Ansible.

set -euo pipefail

# Colorize terminal
red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
no_color='\033[0m'

SCRIPT_PATH="$( cd -- "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )"
# The inventory Ansible reads: ANSIBLE_INVENTORY wins over the product's own.
INVENTORY_DIR="${ANSIBLE_INVENTORY:-$SCRIPT_PATH/../ansible/inventory}"
ALL_VARS_FILE="$INVENTORY_DIR/group_vars/all.yml"
PI_PASSWORD_FILE="$INVENTORY_DIR/group_vars/.pi_password"

# Variables
DOWNLOAD_DIRECTORY="./tmp"
IMAGE_URL="https://downloads.raspberrypi.com/raspios_lite_arm64/images/raspios_lite_arm64-2023-12-11/2023-12-11-raspios-bookworm-arm64-lite.img.xz"
IMAGE_BASE_NAME="${IMAGE_URL##*/}"
IMAGE_BASE_PATH="${DOWNLOAD_DIRECTORY}/${IMAGE_BASE_NAME}"
IMAGE_NAME="${IMAGE_BASE_NAME%.*}"
IMAGE_PATH="${DOWNLOAD_DIRECTORY}/${IMAGE_NAME}"

# Functions
download_image () {
  printf '\n\n%b[Pi image manager].%b Download RaspiOS image.\n\n' "$red" "$no_color"

  mkdir -p "$DOWNLOAD_DIRECTORY"
  curl -fL "$IMAGE_URL" -o "$IMAGE_BASE_PATH"
  curl -fL "$IMAGE_URL.sha256" -o "$IMAGE_BASE_PATH.sha256"
  # macOS has no sha256sum; shasum ships with the OS. The .sha256 file
  # references the bare filename, so check from inside the directory.
  (cd "$DOWNLOAD_DIRECTORY" && shasum -a 256 -c "$IMAGE_BASE_NAME.sha256")
  xz -d -v "$IMAGE_BASE_PATH"
}

copy_image () {
  # /dev/diskN -> /dev/rdiskN: the raw device bypasses the buffer cache and
  # is dramatically faster for sequential writes on macOS.
  local raw_disk="${2/\/dev\/disk//dev/rdisk}"
  printf '  > writing image %s to %s - %bPending..%b\n' "$1" "$raw_disk" "$yellow" "$no_color"

  sudo diskutil unmountDisk "$2"
  sudo dd if="$1" of="$raw_disk" bs=8m status=progress

  printf '  > writing image %s to %s - %bOk%b\n' "$1" "$raw_disk" "$green" "$no_color"
}


# Run script
if [ ! -f "$IMAGE_PATH" ]; then
  download_image
fi


printf '\n\n%b[Pi image manager].%b Copy image to external disks.\n\n' "$red" "$no_color"

TARGET_DISKS=()
while IFS= read -r disk; do
  TARGET_DISKS+=("$disk")
done < <(diskutil list | grep '/dev' | grep 'external' | awk '{print $1}')

if [ "${#TARGET_DISKS[@]}" -eq 0 ]; then
  printf '%b[Pi image manager].%b No external disk found, nothing to do.\n' "$yellow" "$no_color"
  exit 0
fi

# dd to a whole disk is unrecoverable — never write without an explicit ack.
printf '%b[Pi image manager].%b The following disks will be ERASED:\n' "$yellow" "$no_color"
for disk in "${TARGET_DISKS[@]}"; do
  diskutil info "$disk" | grep -E 'Device Identifier|Device / Media Name|Disk Size' | sed 's/^ */    /'
  printf '\n'
done
read -r -p "Type 'yes' to erase and flash ALL disks listed above: " CONFIRM
if [ "$CONFIRM" != "yes" ]; then
  printf '%b[Pi image manager].%b Aborted.\n' "$red" "$no_color"
  exit 1
fi
# Cache sudo credentials once — the parallel dd jobs below would otherwise
# race each other on the password prompt.
sudo -v

JOBS=()
for disk in "${TARGET_DISKS[@]}"; do
  copy_image "$IMAGE_PATH" "$disk" &
  JOBS+=($!)
done


printf '\n\n%b[Pi image manager].%b Wait for copy to be complete.\n\n' "$red" "$no_color"

for job in "${JOBS[@]}"; do
  printf '  > waiting for job: %s - %bPending..%b\n' "$job" "$yellow" "$no_color"
  wait "$job"
  printf '  > waiting for job: %s - %bOk%b\n\n' "$job" "$green" "$no_color"
done


printf '\n\n%b[Pi image manager].%b Seed ssh + user credentials on boot partitions.\n\n' "$red" "$no_color"

USERNAME="$(yq '.ansible_user' "$ALL_VARS_FILE")"
if [ -z "$USERNAME" ] || [ "$USERNAME" = "null" ]; then
  USERNAME="debian"
  export USERNAME
  yq -i '.ansible_user = env(USERNAME)' "$ALL_VARS_FILE"
fi
export USERNAME

# The inventory indirects the password through ansible-vault
# (ansible_password: "{{ vault_ansible_password }}"), so all.yml never holds
# the real value — and this script must never write one there either (the
# file is git-tracked). Reuse the previously generated password when present,
# otherwise generate one and persist it in a gitignored file.
if [ -f "$PI_PASSWORD_FILE" ]; then
  PASSWORD="$(cat "$PI_PASSWORD_FILE")"
else
  PASSWORD="$(openssl rand -base64 15)"
  (umask 177 && printf '%s' "$PASSWORD" > "$PI_PASSWORD_FILE")
  printf '%b[Pi image manager].%b Generated OS password stored in %s\n' "$yellow" "$no_color" "$PI_PASSWORD_FILE"
  printf '%b[Pi image manager].%b Put it in the vault:  ansible-vault edit inventory/group_vars/vault.yml  (vault_ansible_password)\n' "$yellow" "$no_color"
fi
export PASSWORD

find /Volumes -type d -name 'bootfs*' -maxdepth 1 -exec sh -c '
  for volume do
    touch "$volume/ssh"
    ENCRYPTED_PASSWORD="$(printf "%s" "$PASSWORD" | openssl passwd -6 -stdin)"
    printf "%s:%s\n" "$USERNAME" "$ENCRYPTED_PASSWORD" > "$volume/userconf.txt"
    printf "dtoverlay=disable-wifi\ndtoverlay=disable-bt\n" >> "$volume/config.txt"
  done
' exec-sh {} +

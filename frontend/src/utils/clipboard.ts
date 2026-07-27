export async function copyText(value: string): Promise<void> {
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(value);
    return;
  }
  const input = document.createElement("textarea");
  input.value = value;
  input.style.position = "fixed";
  input.style.opacity = "0";
  document.body.appendChild(input);
  input.select();
  const copied = document.execCommand("copy");
  input.remove();
  if (!copied) throw new Error("复制失败，请手动复制");
}

export function installationCommand(token: string): string {
  const protocol = location.protocol === "https:" ? "wss" : "ws";
  const reportUrl = `${protocol}://${location.host}/report`;
  return `curl -fsSL https://raw.githubusercontent.com/M1saka10010/SwallowAgent/main/install.sh | bash -s -- ${reportUrl} ${token}`;
}

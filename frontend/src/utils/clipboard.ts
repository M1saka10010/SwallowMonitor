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

function shellQuote(value: string): string {
  return "'" + value.replaceAll("'", "'\"'\"'") + "'";
}

export function installationCommand(token: string): string {
  const protocol = location.protocol === "https:" ? "wss" : "ws";
  const reportUrl = `${protocol}://${location.host}/report`;
  const scriptUrl = "https://raw.githubusercontent.com/M1saka10010/SwallowAgent/e430832927629f9c228b92558f409a65b589d7f0/install.sh";
  return `curl '-fsSL' ${shellQuote(scriptUrl)} | bash '-s' '--' ${shellQuote(reportUrl)} ${shellQuote(token)}`;
}

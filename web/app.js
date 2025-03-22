function connectWebSocket() {
  const ws = new WebSocket(wsURL);

  ws.onopen = function (evt) {
    handleLog(`${now()} connected`);
  };
  ws.onclose = function (evt) {
    handleLog(`${now()} disconnected`);
    setTimeout(connectWebSocket, 2000);
  };
  ws.onmessage = function (evt) {
    const { kind, body } = JSON.parse(evt.data);

    switch (kind) {
      case "stat":
        handleStat(body);
        break;
      case "log":
        handleLog(body);
        break;
      default:
        console.warn(`Unknown payload's kind "${kind}"`);
    }
  };
  ws.onerror = function (evt) {
    handleLog(`${now()} error ${evt.data}`);
  };
}

function handleStat(j) {
  const t = document.getElementById("servers");

  const {
    elapsed,
    targets,
    rpm,
    processed,
    servers,
  } = j;
  const eta = Math.round((targets - processed) / rpm);

  const progress = `
          <div>${Math.round((processed * 100) / targets)}% / ~${eta}min. </div>
          <div>${processed} / ${targets} / ${elapsed}</div>
        `;

  document.getElementById("progress").innerHTML = progress;
  document.getElementById("rpm").textContent = `${rpm}`;

  if (servers) {
    document.getElementById('proxies').textContent = `${Object.keys(servers).length}`;

    t.innerHTML = `
      <tr>
        <th></th>
        <th>URL</th>
        <th>Latency (sec)</th>
        <th>Efficiency (%)</th>
        <th>Capacity</th>
        <th>Requests</th>
        <th>Positive</th>
        <th>Negative</th>
      </tr>
    `;

    Object.values(servers)
      .sort((a, b) => b.positive - a.positive)
      .forEach(({ url, disabled, latency, efficiency, capacity, requests, positive, negative }, idx) => {
        const row = document.createElement("tr");

        // row.classList.add(disabled ? "disabled" : "");

        row.innerHTML = `
          <td>${idx + 1}.</td>
          <td class="host">${url}</td>
          <td class="">${(latency / 1000).toFixed(1)}</td>
          <td class="">${efficiency}</td>
          <td class="">${capacity}</td>
          <td class="">${requests}</td>
          <td class="positive">${positive}</td>
          <td class="negative">${negative}</td>
        `;
        t.appendChild(row);
      });
  }
}

function handleLog(text) {
  const l = document.getElementById("log");
  const p = document.createElement("p");
  p.innerText = text;
  l.insertBefore(p, l.firstChild);
}

// Returns the current time
function now() {
  let now = new Date();

  let day = String(now.getDate()).padStart(2, "0");
  let month = String(now.getMonth() + 1).padStart(2, "0");
  let year = now.getFullYear();

  let hours = String(now.getHours()).padStart(2, "0");
  let minutes = String(now.getMinutes()).padStart(2, "0");
  let seconds = String(now.getSeconds()).padStart(2, "0");

  return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`;
}

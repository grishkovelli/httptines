function connectWebSocket() {
  const ws = new WebSocket(wsHost);

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
  const t = document.getElementById("balancer");
  const eta = Math.round((j.targets - j.processed) / j.RPM);

  const progress = `
          <div>${Math.round(
            (j.processed * 100) / j.targets
          )}% / ~${eta}min. </div>
          <div>${j.processed} / ${j.targets}</div>
        `;

  document.getElementById("progress").innerHTML = progress;
  document.getElementById("rpm").textContent = `${j.RPM}`;
  document.getElementById("waiting").textContent = `${j.waiting}`;

  if (j.balancer?.alive) {
    document.getElementById(
      "proxies"
    ).textContent = `${j.balancer.alive.length} / ${j.balancer.proxies}`;
  }

  if (j.balancer?.alive) {
    t.innerHTML = `
            <tr>
              <th></th>
              <th>URL</th>
              <th>Latency (sec)</th>
              <th>Capacity</th>
              <th>Requests</th>
              <th>Limit</th>
              <th>Positive</th>
              <th>Negative</th>
            </tr>
          `;
    j.balancer.alive
      .sort((a, b) => a.latency - b.latency)
      .forEach(
        (
          {
            url,
            weight,
            capacity,
            latency,
            requests,
            limit,
            positive,
            negative,
          },
          idx
        ) => {
          const row = document.createElement("tr");
          const trClass = negative >= positive ? positive : "cross";

          row.innerHTML = `
                <tr class="${trClass}">
                  <td>${idx + 1}.</td>
                  <td class="host">${url}</td>
                  <td class="">${(latency / 1000).toFixed(1)}</td>
                  <td class="">${capacity}</td>
                  <td class="">${requests}</td>
                  <td class="">${limit}</td>
                  <td class="positive">${positive}</td>
                  <td class="negative">${negative}</td>
                </tr>
              `;
          t.appendChild(row);
        }
      );
  }
}

function handleLog(text) {
  const l = document.getElementById("log");
  const p = document.createElement("p");
  p.innerText = text;
  l.insertBefore(p, l.firstChild);
}

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

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
  const t = document.getElementById("balancer");

  if (!j?.balancer) {
    return;
  }

  const { targets, waiting, balancer: {
    alive, proxies, rpm, positive
  } } = j;
  const eta = Math.round((targets - positive) / rpm);

  const progress = `
          <div>${Math.round(
            (positive * 100) / targets
          )}% / ~${eta}min. </div>
          <div>${positive} / ${targets}</div>
        `;

  document.getElementById("progress").innerHTML = progress;
  document.getElementById("rpm").textContent = `${rpm}`;
  document.getElementById("waiting").textContent = `${waiting}`;

  if (alive) {
    document.getElementById('proxies').textContent = `${alive.length} / ${proxies}`;

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
    alive
      .sort((a, b) => a.latency - b.latency)
      .forEach(
        (
          {
            url,
            capacity,
            latency,
            requests,
            limit,
            positive,
            negative,
            negativePct,
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
                  <td class="negative">${negativePct}% (${negative})</td>
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

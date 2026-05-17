<?php
$phpVersion = phpversion();
$serverSoftware = $_SERVER['SERVER_SOFTWARE'] ?? 'Unknown';
$projectsPath = '{{PROJECTS_PATH}}';
?>
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Devour — Local Development Environment</title>
  <style>
    * { margin: 0; padding: 0; box-sizing: border-box; }
    body {
      background: #0f1010;
      color: #e8e8e6;
      font-family: 'Segoe UI', -apple-system, BlinkMacSystemFont, sans-serif;
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
    }
    .container {
      max-width: 560px;
      width: 100%;
      padding: 24px;
    }
    .card {
      background: #1a1c1a;
      border: 1px solid #252720;
      border-radius: 16px;
      padding: 48px 40px;
      text-align: center;
    }
    .logo {
      font-size: 32px;
      font-weight: 800;
      letter-spacing: -0.5px;
      color: #b5f23d;
      margin-bottom: 8px;
    }
    .logo span { color: #6b7068; font-weight: 400; font-size: 14px; margin-left: 8px; }
    .subtitle {
      color: #6b7068;
      font-size: 14px;
      margin-bottom: 32px;
    }
    .info-grid {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 12px;
      margin-bottom: 32px;
    }
    .info-box {
      background: #0f1010;
      border: 1px solid #252720;
      border-radius: 10px;
      padding: 16px;
      text-align: left;
    }
    .info-box .label {
      color: #6b7068;
      font-size: 11px;
      text-transform: uppercase;
      letter-spacing: 0.5px;
      margin-bottom: 6px;
    }
    .info-box .value {
      color: #e8e8e6;
      font-family: 'Cascadia Code', 'Fira Code', monospace;
      font-size: 14px;
    }
    .info-box .value.accent { color: #b5f23d; }
    .hint {
      background: #0f1010;
      border: 1px solid #252720;
      border-radius: 10px;
      padding: 20px;
      text-align: left;
    }
    .hint .title {
      color: #b5f23d;
      font-size: 13px;
      font-weight: 600;
      margin-bottom: 8px;
    }
    .hint p {
      color: #6b7068;
      font-size: 13px;
      line-height: 1.6;
    }
    .hint code {
      background: #1a1c1a;
      color: #b5f23d;
      padding: 2px 8px;
      border-radius: 4px;
      font-size: 12px;
      font-family: 'Cascadia Code', 'Fira Code', monospace;
    }
    .footer {
      text-align: center;
      margin-top: 24px;
      color: #3a3d38;
      font-size: 12px;
    }
  </style>
</head>
<body>
  <div class="container">
    <div class="card">
      <div class="logo">DEVOUR <span>v1.0</span></div>
      <div class="subtitle">Your local development environment is running.</div>
      <div class="info-grid">
        <div class="info-box">
          <div class="label">PHP Version</div>
          <div class="value accent"><?php echo $phpVersion; ?></div>
        </div>
        <div class="info-box">
          <div class="label">Server</div>
          <div class="value"><?php echo htmlspecialchars($serverSoftware); ?></div>
        </div>
      </div>
      <div class="hint">
        <div class="title">Getting Started</div>
        <p>
          Place your project folders in<br>
          <code><?php echo htmlspecialchars($projectsPath); ?></code><br><br>
          Each folder automatically gets its own domain:<br>
          <code>myproject</code> → <code>http://myproject.test</code>
        </p>
      </div>
    </div>
    <div class="footer">Devour — Faster than everything.</div>
  </div>
</body>
</html>

<!DOCTYPE html>
<html lang="en" dir="ltr">
  <header>
    <meta charset="utf-8" />
    <meta http-equiv="X-UA-Compatible" content="IE=Edge" />
    <title>GSKY Catalogues</title>
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <meta name="robots" content="index, follow" />
    <style>
      body {
        font-family: "Segoe UI", "Fira Sans", "Droid Sans", "Helvetica Neue",
          "Arial", "sans-serif";
        font-size: 16px;
      }

      #header {
        padding-top: 18px;
        padding-bottom: 18px;
        margin-top: 0px;
        padding-left: 0px;
        padding-right: 0px;
        margin-bottom: 15px;
        background-color: #fafbfc;
        border-bottom: 0.5px solid #eeeeee;
      }

      #header_container {
        width: 980px;
        margin-left: auto;
        margin-right: auto;
      }

      #header_container h2 {
        color: #24292e;
      }

      #container {
        width: 980px;
        margin-left: auto;
        margin-right: auto;
      }

      ul {
        list-style-type: none;
        margin: 0;
        padding: 0;
      }

      .list li {
        padding-top: 10px;
        margin-left: 20px;
        padding-bottom: 10px;
        border-bottom: 0.4px solid #eeeeee;
      }

      .ns-title {
        display: flex;
        align-items: center;
      }

      .ns-title h4 {
        padding: 4px 8px;
        margin: 0;
      }

      .layer div {
        padding: 4px 0;
      }

      .layer label {
        padding-right: 4px;
      }

      .layer-item {
        display: flex;
        align-items: center;
      }

      .layer-timestamps {
        display: flex;
        align-items: center;
      }

      .sub-list li {
        border-bottom: none;
      }

      a {
        text-decoration: none;
        display: block;
        padding-left: 5px;
      }

      a:link,
      a:visited {
        color: #0366d6;
      }

      a:hover {
        color: #0366d6;
        text-decoration: underline;
      }
    </style>
  </header>
  <body>
    <div id="header">
      <div id="header_container">
        <h2>GSKY Catalogues</h2>
      </div>
    </div>
    <div id="container">
      <ul class="list">
        {{ range $index, $config := . }}
          <li>
            <div class="ns-title">
              <h4>Namespace: {{ $config.Title }}</h4>
              <a href="{{ $config.GetCapabilitiesLink.URL }}">{{ $config.GetCapabilitiesLink.Title }}</a>
            </div>
            <ul class="sub-list">
              {{ range $ilayer, $layer := $config.Layers }}
                <li>
                  <div class="layer">
                    <div class="layer-item">
                      <label>Layer Name:</label>
                      <div>{{ $layer.Name }}</div>
                    </div>
                    <div class="layer-item">
                      <label>Timestamps Link:</label>
                      <a href="{{ $layer.TimestampsLink.URL }}">{{ $layer.TimestampsLink.Title}}</a>
                    </div>
                  </div>
                </li>
              {{ end }}
            </ul>
          </li>
        {{ end }}
      </ul>
    </div>
  </body>
</html>

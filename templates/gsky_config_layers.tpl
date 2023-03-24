<!DOCTYPE html>
<html lang="en" dir="ltr">
  <header>
    <meta charset="utf-8" />
    <meta http-equiv="X-UA-Compatible" content="IE=Edge" />
    <title>GSKY Catalogues</title>
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <meta name="robots" content="index, follow" />
    <link href='https://unpkg.com/maplibre-gl@2.4.0/dist/maplibre-gl.css' rel='stylesheet' />
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

      .map-toggle-btn{
        cursor: pointer;
      }

      .map {
        display: none;
        margin-top: 20px;
        height: 400px;
        width: 100%;
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
                    <div class="layer-preview">
                      <button class="map-toggle-btn">Show Map</button>
                      <div class="map" id="sconc_dust" data-owsbaseurl="{{ $config.GetCapabilitiesLink.URL }}" data-timestampsurl="{{ $layer.TimestampsLink.URL }}"></div>
                   </div>
                  </div>
                </li>
              {{ end }}
            </ul>
          </li>
        {{ end }}
      </ul>
    </div>
    <script src="https://unpkg.com/maplibre-gl@2.4.0/dist/maplibre-gl.js"></script>
    <script src="https://code.jquery.com/jquery-3.6.4.min.js" integrity="sha256-oP6HI9z1XaZNBrJURtCoUT5SUnxFr8s3BzRl+cbzUq8=" crossorigin="anonymous"></script>
    <script>
      const fetchTimestamps = (url) => {
        return fetch(url)
          .then((res) => res.json())
          .then((data) => data.timestamps);
      };
    
      const defaultStyle = {
        version: 8,
        sources: {
          "carto-light": {
            type: "raster",
            tiles: [
              "https://a.basemaps.cartocdn.com/light_all/{z}/{x}/{y}@2x.png",
              "https://b.basemaps.cartocdn.com/light_all/{z}/{x}/{y}@2x.png",
              "https://c.basemaps.cartocdn.com/light_all/{z}/{x}/{y}@2x.png",
              "https://d.basemaps.cartocdn.com/light_all/{z}/{x}/{y}@2x.png",
            ],
          },
          wikimedia: {
            type: "raster",
            tiles: ["https://maps.wikimedia.org/osm-intl/{z}/{x}/{y}.png"],
          },
        },
        layers: [
          {
            id: "carto-light-layer",
            source: "carto-light",
            type: "raster",
            minzoom: 0,
            maxzoom: 22,
          },
        ],
      };
    
      $(document).ready(function () {
        $(".map-toggle-btn").click(function () {
          const $mapEl = $(this).siblings(".map");
    
          if ($mapEl) {
            if ($mapEl.is(":visible")) {
              // hide map
              $mapEl.hide();
              $(this).html("Show Map");
            } else {
              const mapId = $mapEl.attr("id");
    
              const owsBaseUrl = $mapEl.data("owsbaseurl");
              const timestampsUrl = $mapEl.data("timestampsurl");
    
              $mapEl.show();
    
              const map = new maplibregl.Map({
                container: mapId,
                style: defaultStyle,
                center: [36.246, 5.631],
                zoom: 3,
                hash: true,
              });
    
              const tileUrlBase = new URL(owsBaseUrl);
              tileUrlBase.searchParams = { h: "hello" };
    
              const tileParams = {
                service: "WMS",
                request: "GetMap",
                version: "1.1.1",
                width: 512,
                height: 512,
                transparent: true,
                srs: "EPSG:3857",
                bbox: "{bbox-epsg-3857}",
                format: "image/png",
                layers: mapId,
              };
    
              map.on("load", async () => {
                const timeStamps = await fetchTimestamps(timestampsUrl);
    
                tileParams.time = timeStamps[timeStamps.length - 1];
    
                const qs = new URLSearchParams(tileParams).toString();
                tileUrlBase.search = decodeURIComponent(qs);
    
                map.addSource(mapId, {
                  type: "raster",
                  maxzoom: 5,
                  tiles: [tileUrlBase.href],
                });
    
                map.addLayer({
                  id: mapId,
                  type: "raster",
                  source: mapId,
                });
              });
    
              $(this).html("Hide Map");
            }
          }
        });
      });
    </script>    
  </body>
</html>

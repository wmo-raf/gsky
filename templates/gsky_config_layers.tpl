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

      .timestamp-picker{
        position: relative;
      }
      
      .timestamp-picker select{
        cursor: pointer;
        position: absolute;
        top:12px;
        left: 12px;
        z-index: 999;
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
                      <div class="map" id="{{ $layer.Name }}" data-owsbaseurl="{{ $config.GetCapabilitiesLink.URL }}" data-timestampsurl="{{ $layer.TimestampsLink.URL }}">
                        <div class="timestamp-picker">
                          <select>
                          </select>
                        </div>
                      </div>
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
    
      function extractBoundingBox(wkt) {
        // Extract the coordinates from the WKT string
        const coords = wkt.match(/-?\d+\.\d+/g).map(parseFloat);
    
        // Find the min/max values for the x and y coordinates
        let minX = Infinity,
          minY = Infinity,
          maxX = -Infinity,
          maxY = -Infinity;
        for (let i = 0; i < coords.length; i += 2) {
          minX = Math.min(minX, coords[i]);
          minY = Math.min(minY, coords[i + 1]);
          maxX = Math.max(maxX, coords[i]);
          maxY = Math.max(maxY, coords[i + 1]);
        }
    
        // Return the bounding box as an array
        return [minX, minY, maxX, maxY];
      }

      function validateBoundingBox(bbox) {
        const [minX, minY, maxX, maxY] = bbox;
        
        // Check if bounding box is valid
        if (minX < -180 || minY < -90 || maxX > 180 || maxY > 90) {
          // Return world extents
          return [-180, -90, 180, 90];
        }
        
        // Return the original bounding box
        return bbox;
      }
    
      const getDataBbox = (url) => {
        return fetch(url)
          .then((res) => res.json())
          .then((data) => {
            if (data.gdal && !!data.gdal.length) {
              const polygon = data.gdal[0].polygon;
              if (!polygon) {
                return null;
              }
              const bbox = extractBoundingBox(polygon);
              return bbox;
            } else {
              return null;
            }
          });
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
    
      const onTimeChange = (selectedTime, map, sourceId) => {
        if (selectedTime && map && sourceId) {
          const source = map.getSource(sourceId);
          const tileUrl = new URL(source.tiles[0]);
    
          const qs = new URLSearchParams(tileUrl.search);
          qs.set("time", selectedTime);
          tileUrl.search = decodeURIComponent(qs);
    
          // adapted from https://github.com/mapbox/mapbox-gl-js/issues/2941#issuecomment-518631078
          map.getSource(sourceId).tiles = [tileUrl.href];
    
          // Remove the tiles for a particular source
          map.style.sourceCaches[sourceId].clearTiles();
    
          // Load the new tiles for the current viewport (map.transform -> viewport)
          map.style.sourceCaches[sourceId].update(map.transform);
    
          // Force a repaint, so that the map will be repainted without you having to touch the map
          map.triggerRepaint();
        }
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
              const timestampSelect = $($mapEl).find("select");
    
              const owsBaseUrl = $mapEl.data("owsbaseurl");
              const timestampsUrl = $mapEl.data("timestampsurl");
              const metadataUrl = new URL(timestampsUrl);
              metadataUrl.search = "";
    
              $mapEl.show();
    
              const map = new maplibregl.Map({
                container: mapId,
                style: defaultStyle,
                center: [36.246, 5.631],
                zoom: 3,
                hash: true,
              });
    
              const tileUrlBase = new URL(owsBaseUrl);
    
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
    
                if (timeStamps && !!timeStamps.length) {
                  // reverse to show the latest first
                  timeStamps.reverse();
    
                  const latestTime = timeStamps[0];
    
                  if (timestampSelect) {
                    timestampSelect.val(latestTime);
    
                    timeStamps.forEach((timestamp) => {
                      $(timestampSelect).append(new Option(timestamp, timestamp));
                    });
    
                    timestampSelect.on("change", function (e) {
                      const selectedTime = e.target.value;
                      onTimeChange(selectedTime, map, mapId);
                    });
                  }
    
                  if (latestTime) {
                    const metadataParams = {
                      intersects: true,
                      metadata: "gdal",
                      time: latestTime,
                    };
    
                    const qs = new URLSearchParams(metadataParams).toString();
                    metadataUrl.search = decodeURIComponent(qs);
    
                    const bbox = await getDataBbox(metadataUrl);
                    const validBbox = validateBoundingBox(bbox);
    
                    if (validBbox) {
                      // fit to bounds
                      map.fitBounds(validBbox, { padding: 20 });
                    }
                  }
    
                  tileParams.time = latestTime;
    
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
                }
              });
    
              $(this).html("Hide Map");
            }
          }
        });
      });
    </script>
  </body>
</html>

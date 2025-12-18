# Stitch API Usage Examples

## Simple Bounding Box Request

```bash
curl -X POST http://localhost:8080/api/v1/stitch \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "bbox",
    "bbox": {
      "min_lat": 37.371794,
      "min_lon": -122.917099,
      "max_lat": 38.226853,
      "max_lon": -121.564407
    },
    "zoom": 10,
    "tile_source": {
      "url": "http://a.tile.openstreetmap.org/{z}/{x}/{y}.png",
      "name": "OpenStreetMap"
    }
  }' \
  --output san_francisco_bay.png
```

## Centered Mode Request

```bash
curl -X POST http://localhost:8080/api/v1/stitch \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "centered",
    "center": {
      "lat": 35.6824,
      "lon": 139.7531,
      "width": 640,
      "height": 480
    },
    "zoom": 10,
    "tile_source": {
      "url": "http://b.tile.stamen.com/watercolor/{z}/{x}/{y}.jpg",
      "name": "Stamen Watercolor"
    }
  }' \
  --output tokyo_watercolor.png
```

## Request with World File Generation

```bash
curl -X POST http://localhost:8080/api/v1/stitch \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "bbox",
    "bbox": {
      "min_lat": 37.37,
      "min_lon": -122.92,
      "max_lat": 38.23,
      "max_lon": -121.56
    },
    "zoom": 10,
    "tile_source": {
      "url": "http://a.tile.openstreetmap.org/{z}/{x}/{y}.png"
    },
    "output": {
      "format": "png",
      "generate_worldfile": true
    }
  }' \
  --output map_with_worldfile.png
```

## Request with Custom Headers

```bash
curl -X POST http://localhost:8080/api/v1/stitch \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "bbox",
    "bbox": {
      "min_lat": 50.88,
      "min_lon": 6.88,
      "max_lat": 50.98,
      "max_lon": 7.04
    },
    "zoom": 14,
    "tile_source": {
      "url": "http://b.tile.stamen.com/toner/{z}/{x}/{y}@2x.png",
      "name": "Stamen Toner Retina",
      "headers": {
        "User-Agent": "MyApp/1.0",
        "Referer": "https://myapp.com"
      }
    },
    "output": {
      "tile_size": 512
    }
  }' \
  --output cologne_retina.png
```

## Health Check

```bash
curl http://localhost:8080/api/v1/health
```

Response:
```json
{
  "status": "healthy",
  "timestamp": "2024-01-15T10:30:00Z",
  "version": "2.0.0",
  "uptime": 3600
}
```

## Error Handling Examples

### Invalid Coordinates
```bash
curl -X POST http://localhost:8080/api/v1/stitch \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "bbox",
    "bbox": {
      "min_lat": 38.0,
      "min_lon": -122.0,
      "max_lat": 37.0,
      "max_lon": -121.0
    },
    "zoom": 10,
    "tile_source": {
      "url": "http://a.tile.openstreetmap.org/{z}/{x}/{y}.png"
    }
  }'
```

Response (400 Bad Request):
```json
{
  "error": "INVALID_COORDINATES",
  "message": "max_lat must be greater than min_lat",
  "request_id": "req_123456789"
}
```

### Missing Required Fields
```bash
curl -X POST http://localhost:8080/api/v1/stitch \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "bbox",
    "bbox": {
      "min_lat": 37.37,
      "min_lon": -122.92,
      "max_lat": 38.23,
      "max_lon": -121.56
    }
  }'
```

Response (422 Validation Error):
```json
{
  "error": "VALIDATION_ERROR",
  "message": "Request validation failed",
  "validation_errors": [
    {
      "field": "zoom",
      "message": "zoom is required",
      "code": "REQUIRED"
    },
    {
      "field": "tile_source",
      "message": "tile_source is required",
      "code": "REQUIRED"
    }
  ],
  "request_id": "req_123456789"
}
```

### Tile Server Error
If some tiles fail to download, you'll get a 502 response:

```json
{
  "error": "TILE_SERVER_ERROR",
  "message": "Failed to download tiles from tile server",
  "failed_tiles": [
    {
      "url": "http://a.tile.openstreetmap.org/10/163/395.png",
      "status_code": 404,
      "error": "Tile not found"
    }
  ],
  "successful_tiles": 8,
  "total_tiles": 10,
  "request_id": "req_123456789"
}
```
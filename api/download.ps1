$url =  "https://raw.githubusercontent.com/immich-app/immich/refs/heads/main/open-api/immich-openapi-specs.json"
$outFile = Join-Path -Path $PSScriptRoot -ChildPath "immich-openapi-specs.json"
Invoke-WebRequest -Uri $url -OutFile $outFile
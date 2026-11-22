$version = "v3.4.2"
$url =  "https://raw.githubusercontent.com/immich-app/immich/refs/tags/$version/open-api/immich-openapi-specs.json"
$outFile = Join-Path -Path $PSScriptRoot -ChildPath "immich-openapi-specs.json"
Invoke-WebRequest -Uri $url -OutFile $outFile

Write-Host "Downloaded Immich OpenAPI specs version $version to $outFile"
Write-Host "Consider to run 'go generate ./...' to update the generated code"
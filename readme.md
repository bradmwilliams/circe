Workflow:
- Make your changes to guide.yaml
- Run `rm -rf render/src/generated/java/com && go run circe.go`
- Commit and push ./render
- Commit and push .
- Pull continuous-release-jobs/config/render
- Commit & push continuous-release-jobs
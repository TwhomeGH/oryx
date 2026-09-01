.PHONY: default clean build
.PHONY: npm lint test

default: build

build: npm
	env PUBLIC_URL=/mgmt BUILD_PATH=build npm run build

npm: lint
	npm install

# Don't run lint in GitHub Actions, because it crashes the build.
# See https://docs.github.com/en/actions/learn-github-actions/variables#default-environment-variables
lint:
	@if [ -z "$(GITHUB_ACTION)" ] && [ -z "$(SRS_NO_LINT)" ]; then npm run lint; fi

test: lint
	npm run test

clean:
	rm -rf build

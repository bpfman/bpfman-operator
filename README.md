![bpfman logo](https://github.com/bpfman/bpfman/blob/main/docs/img/horizontal/color/bpfman-horizontal-color.png)<!-- markdownlint-disable-line first-line-heading -->

# bpfman: An eBPF Manager

[![License][apache2-badge]][apache2-url]
![Build status][build-badge]
[![Book][book-badge]][book-url]
![Project maturity: alpha][project-maturity]
[![Go report card][go-report-card-badge]][go-report-card-report]
[![OpenSSF Scorecard][openssf-badge]][openssf-url]
[![FOSSA Status][fossa-badge]][fossa-url]

[apache2-badge]: https://img.shields.io/badge/License-Apache%202.0-blue.svg
[apache2-url]: https://opensource.org/licenses/Apache-2.0
[build-badge]: https://img.shields.io/github/actions/workflow/status/bpfman/bpfman-operator/image-build.yaml?branch=main
[book-badge]: https://img.shields.io/badge/read%20the-book-9cf.svg
[book-url]: https://bpfman.io/
[project-maturity]: https://img.shields.io/badge/maturity-alpha-orange.svg
[go-report-card-badge]: https://goreportcard.com/badge/github.com/bpfman/bpfman-operator
[go-report-card-report]: https://goreportcard.com/report/github.com/bpfman/bpfman-operator
[openssf-badge]: https://api.scorecard.dev/projects/github.com/bpfman/bpfman-operator/badge
[openssf-url]: https://scorecard.dev/viewer/?uri=github.com/bpfman/bpfman-operator
[fossa-badge]: https://app.fossa.com/api/projects/git%2Bgithub.com%2Fbpfman%2Fbpfman-operator.svg?type=shield
[fossa-url]: https://app.fossa.com/projects/git%2Bgithub.com%2Fbpfman%2Fbpfman-operator?ref=badge_shield

bpfman is a Cloud Native Computing Foundation Sandbox project

<picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/cncf/artwork/main/other/cncf/horizontal/white/cncf-white.png"/>
   <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/cncf/artwork/main/other/cncf/horizontal/color/cncf-color.png"/>
   <img alt="CNCF Logo" src="https://raw.githubusercontent.com/cncf/artwork/main/other/cncf/horizontal/color/cncf-color.png" width="200px"/>
</picture>

## Welcome to bpfman-operator

The `bpfman-operator` repository exists to deploy and manage [bpfman](https://github.com/bpfman/bpfman)
within a Kubernetes cluster.
This operator was built using some great tooling provided by the
[operator-sdk library](https://sdk.operatorframework.io/).

Here are some links to help in your bpfman journey (all links are from the bpfman website <https://bpfman.io/>):

- [Welcome to bpfman](https://bpfman.io/main/) for overview of bpfman.
- [Deploying Example eBPF Programs On Kubernetes](https://bpfman.io/main/getting-started/example-bpf-k8s/)
  for some examples of deploying eBPF programs through `bpfman` in a Kubernetes deployment.
- [Setup and Building bpfman](https://bpfman.io/main/getting-started/building-bpfman/) for instructions
  on setting up your development environment and building bpfman.
- [Example eBPF Programs](https://bpfman.io/main/getting-started/example-bpf/) for some
  examples of eBPF programs written in Go, interacting with `bpfman`.
- [Deploying the bpfman-operator](https://bpfman.io/main/getting-started/operator-quick-start/) for details on launching
  bpfman in a Kubernetes cluster.
- [Developing the bpfman-operator](https://bpfman.io/main/developer-guide/develop-operator/) for more architecture
  details about `bpfman-operator` and details on developing bpfman-operator.
- [Meet the Community](https://bpfman.io/main/governance/meetings/) for details on community meeting details.

## Star History

<a href="https://star-history.com/#bpfman/bpfman-operator&Date">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=bpfman/bpfman-operator&type=Date&theme=dark" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=bpfman/bpfman-operator&type=Date" />
   <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=bpfman/bpfman-operator&type=Date" />
 </picture>
</a>

## License

[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Fbpfman%2Fbpfman-operator.svg?type=large)](https://app.fossa.com/projects/git%2Bgithub.com%2Fbpfman%2Fbpfman-operator?ref=badge_large)

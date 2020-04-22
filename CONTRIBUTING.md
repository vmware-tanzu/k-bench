# Contributing to K-Bench

Before you start to contribute to K-Bench, please read our [Developer Certificate of Origin](https://cla.vmware.com/dco). 
All contributions to this repository must be signed as described on that page. Your signature certifies 
that you wrote the patch or have the right to pass it on as an open-source patch.

## Community and Communication

K-Bench's success relies on community support and feedback. Users and contributors are welcome to join our 
slack channel (in construction) to discussion K-Bench collaboration and provide feedback:

- [Slack](https://vmwarecode.slack.com/messages/kbench)  (TBD) 

If you find a bug while using K-Bench, or you have an idea or proposal to improve K-Bench project, you may 
also create an issue (and mark it with appropriate kind/category) using the Github's issue tracker.

## Contributors' Guideline

The K-Bench project welcomes contributions from anyone who would like to contribute.  In general,
bug fixes, new features, improvements in code and documentation, extensible or independent modules 
that can be integrated into the current framework are all acceptable changes. 

### Coding Style

All go source files should be formatted using go fmt tool.

### Contribution Workflow

To contribute to the code base, you may submit a pull request after thoroughly testing your change. 
The project maintainers will review and approve the request if everything looks fine with the change.
In particular, below is a high level workflow that contributors should follow to make contributions
to the project:

- Create a new branch from the repository  (e.g., your fork of K-Bench) where you would like to work on the development
- Make changes, run tests, and make commits to your local repo
- Push your changes to the branch in your fork of the repository
- Submit a pull request

Example:

``` shell
git remote add upstream https://github.com/vmware/k-bench.git
git checkout -b my-new-feature master
git commit -a
git push origin my-new-feature
```

### Staying In Sync With Upstream

When your branch gets out of sync with the vmware/master branch, use the following to update:

``` shell
git checkout my-new-feature
git fetch -a
git pull --rebase upstream master
git push --force-with-lease origin my-new-feature
```

### Updating pull requests

If your PR needs changes based on code review or testing, you'll most likely want to squash these 
changes into existing commits.

If your pull request contains a single commit or your changes are related to the most recent commit, 
you can simply amend the commit.

``` shell
git add .
git commit --amend
git push --force-with-lease origin my-new-feature
```

If you need to squash changes into an earlier commit, you can use:

``` shell
git add .
git commit --fixup <commit>
git rebase -i --autosquash master
git push --force-with-lease origin my-new-feature
```

Be sure to add a comment to the PR indicating your new changes are ready to review, as GitHub 
does not generate a notification when you git push.

## Contribution Testing
All code changes have to be tested locally before being pushed upstream. In addition, more 
testing may be conducted before merging into the production branches. K-Bench maintainers
run regular tests and checks to ensure code stability. Any regressions or issues, once detected,
should be reported in the Github issue tracker.

## Reporting Bugs and Creating Issues

You may use the Github issue tracker to report issues or bugs you find while using and testing K-Bench. 
Before reporting the issue, please check whether it is in the existing issue list. 

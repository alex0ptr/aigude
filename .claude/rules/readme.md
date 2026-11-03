---
paths:
  - "README.md"
---

# README

## Follow Diátaxis

Every part of the README does one of the four jobs of [Diátaxis](https://diataxis.fr) and does not mix them in.

| Section | Job | Answers |
| --- | --- | --- |
| Getting Started | Tutorial | Take me by the hand to a first result |
| How-Tos | How-to guides | How do I reach this goal |
| Concepts | Explanation | Why does it work like this |
| the commented example files in `hack/` | Reference | What exactly is there |

A tutorial leads a beginner through numbered steps to one result they can see. It has no choices, no options and no background, and it leaves out everything that is not needed to reach the result. Whatever a reader may want next is one link at the end, not a detour in the middle.

```
bad:  step 2 explains where an API key comes from, three ways to set it and what it costs
good: step 2 links the two places that issue the keys and shows the one command that sets them
```

Reference does not go into the README. Settings and User Config are described in the comments of `hack/aigude.toml` and `hack/users/demo.toml`, and the README links to them.

## No parentheses

Do not put parentheses or other bracketed asides in the prose, in headings or in the labels of diagrams. Say it in a sentence, or leave it out. Link syntax and code are not prose and stay as they are.

```
bad:  The folder users_dir (users by default) holds one file for each User.
good: The folder users_dir holds one file for each User. It is users by default.
```

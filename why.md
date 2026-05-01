# Why recall exists

`recall` exists because personal search is fragmented.

Important information lives in many unrelated places: code, local files, notes, tickets, pull requests, Slack, Notion, GitHub, calendars, shell history, and whatever source gets added next. Each one usually has its own search box, query syntax, authentication story, and command-line habit. The hard part is often not searching one source; it is remembering that the source exists, remembering how to query it, and remembering at the right moment that it might contain the answer.

That creates toil. A simple question turns into source selection, tool selection, syntax recall, and context switching before the real search even starts.

## This is an old problem

Vannevar Bush described the core failure in 1945: the record of human knowledge was growing faster than people's ability to consult it. His proposed `memex` was an "enlarged intimate supplement" to memory: not just more storage, but faster, more flexible ways to reconnect with stored records across sources. The important idea is still current: storing information is not enough if you cannot reliably get back to it when it matters.

Marcia Bates' berrypicking model makes the same point from information-science research. Real searches are rarely one perfect query against one perfect database. People move through multiple sources and techniques, collecting useful bits as the need evolves. Bates also calls out that as more resource types come online, the searcher faces a more complex environment: more sources to consider and more techniques to remember.

Nielsen Norman Group's writing on information scent explains the user-experience version of the same issue. People choose where to search based on cues about whether a source is likely to answer the question and how much effort it will take. If the available sources and search surfaces are not visible and consistently labeled, people will miss useful places to look even when those places contain the answer.

`recall` is built in that context: it is a practical interface for rediscovering the information you already have access to.

## Fragmentation shows up at every scale

There are large-source problems:

- "Was this in Slack, Notion, GitHub, Jira, or local notes?"
- "Which provider do I have configured for this?"
- "What selector do I use for PRs versus code versus pages?"
- "Can this source search title, body, path, comments, or metadata?"

There are also small-tool problems:

- "I want to grep this repo, but not tests."
- "I want path matches, not content matches."
- "I want the same code search from my terminal without remembering my ripgrep incantation."

Historically, the small-tool problem gets solved with aliases, shell functions, copied commands, or project-local scripts. That works for the person who wrote them, until the names drift, the flags are forgotten, or the next source needs a different interface. It is powerful but not discoverable. It also does not compose well with remote sources, structured output, terminal opening, or agents.

The result is a private pile of search tricks instead of a system.

## What recall changes

`recall` gives personal search one integration shape:

- providers expose sources through the same `SearchProvider` contract;
- providers advertise searchable surfaces through `ListCapabilities`;
- selectors name those surfaces consistently, such as `code:file:content` or `gh:pr:content`;
- `recall -ls` makes configured sources and selectors discoverable;
- providers keep source-specific query semantics where they belong;
- recall owns fan-out, validation, ranking, rendering, JSON output, and opening.

That split matters. `recall` does not try to turn every source into the same database. Instead, it gives every source the same front door. Source-specific power stays inside the provider, while the operator gets a consistent way to discover, select, search, inspect, and open results.

For small tools, this means common search needs can graduate from hidden shell folklore into named provider surfaces. A ripgrep provider can expose `file:name` and `file:content`; its query language can keep useful filters like `type:go`, `in:regex`, or `-in:test`; and the operator can discover and route those surfaces the same way they route Slack, GitHub, or Notion.

## Why now

The amount of searchable personal and work data keeps increasing. At the same time, LLMs and coding agents make accessible information more valuable: the better the available context, the better the work they can do. But if information remains split across unlisted tools and private habits, agents inherit the same discovery problem humans have.

`recall` is a way to make sources legible to both humans and tools:

- humans get one command and visible capabilities;
- scripts get structured JSON;
- terminal users get openable links;
- agents get a stable provider model instead of a grab bag of ad hoc commands.

The goal is not one giant index or one universal query language. The goal is lower search activation energy: fewer moments where useful information exists but is not searched because the source, syntax, or command was not obvious.

## The point

`recall` is for turning scattered searchable things into an intentionally discoverable search system.

It should make it easy to add a source, easy to list what can be searched, easy to route a query to the right surfaces, easy to inspect structured results, and easy to open the original thing. It replaces a fragmented collection of search habits with a consistent provider ecosystem while preserving the source-specific behavior that makes each provider useful.

## References

- Vannevar Bush, "As We May Think," *The Atlantic*, 1945. https://www.theatlantic.com/magazine/archive/1945/07/as-we-may-think/303881/
- Marcia J. Bates, "The Design of Browsing and Berrypicking Techniques for the Online Search Interface," 1989. https://pages.gseis.ucla.edu/faculty/bates/berrypicking.html
- Raluca Budiu, "Information Scent: How Users Decide Where to Go Next," Nielsen Norman Group, 2020. https://www.nngroup.com/articles/information-scent/

Map the whole project: code in any common language, plus its documents, images and other assets, stylesheets, markup, SQL, schemas, and build, CI and infrastructure files. It records what each file defines, what it imports, which definitions use which, and which files name which by path. Answers in a few hundred tokens what would take reading many files.

WHEN TO USE THIS TOOL:
- At the start of work in a project you have not seen, or after a compaction: overview shows the subsystems, entry points and the most depended-on files with their main definitions, then the documents, the assets, and broken links.
- Before reading files to find something: find locates a definition by name; explain shows where it is, what uses it and what it uses, so you open only the files that matter.
- Before changing a function's signature or behavior, or deleting it, or renaming, moving or deleting a file, image, CSS class, table or heading: callers lists what the change reaches, level by level.
- To see how two parts connect: path shows the chain of uses between them.
- When a file changes, explain on the file also lists the files that git history shows changing with it, such as its test.

WHAT IT MAPS BESIDES CODE:
- Documents (Markdown, reStructuredText, AsciiDoc): headings as sections, links and embedded images with whether they resolve (a missing file or #anchor is reported), and the code definitions the text names, labeled inferred.
- Assets (images, fonts, audio, video, 3D models, data): format, pixel size and bytes; which files name each one; which ones no file names.
- Styles and markup: CSS, SCSS and Less classes, variables, mixins and keyframes, linked to the HTML, components and scripts that use them; element ids linked to getElementById and querySelector.
- Data and APIs: SQL tables, views and functions, linked to the queries in code that use them; GraphQL, Protobuf, Thrift, Prisma and OpenAPI types; OpenAPI endpoints linked to the requests in code whose path matches them.
- Build and infrastructure: npm scripts, Make and just targets, Taskfile tasks, Dockerfile stages, Compose services, GitHub Actions and GitLab CI jobs, Terraform resources, modules and variables, and the files they run.
- Notebooks: code cells as the kernel's language, Markdown cells as a document.

ACTIONS:
- overview: languages by kind, subsystems (code files grouped by how they use each other), entry points, most depended-on files and their main definitions, then documents, assets (largest, and unnamed ones) and broken links. budget sets its size.
- find: definitions and files whose name matches query, exact names first. Covers headings, CSS classes, tables, scripts and the like.
- explain: a definition (name, Container.Name or path:Name), a file, or an asset. For a definition: its location and signature, who uses it (and for a heading, which links point at it), what it uses, and the view call to read it. For a file: what it defines, its imports and where they resolve, the paths it names and what each resolved to, who uses it, and what changes with it. For an asset: its size and dimensions and every place that names it.
- callers: what changing query affects, up to depth levels. For a file or a heading this starts with the links that name it.
- path: the shortest chain of uses from query to to, else from to to query.

HOW SURE EACH LINK IS:
Every link is labeled.
- resolved: through an import, the same file or package, or the page and stylesheet that load each other. Trust it.
- inferred: the only definition with that name in the project, or a name in prose. Usually right.
- ambiguous: several definitions match, or a method is called on a value of unknown type ("receiver type unknown"). Check before relying on it.

WHAT IT CANNOT SEE:
It reads files without building or running them. Calls through reflection, callbacks stored in variables, dependency injection, generated code and string-based dispatch are not seen; neither are file names built at run time beyond a simple ${...} template, nor classes or ids assembled from strings. In Ruby and Elixir, calls written without parentheses are not seen. Definitions inside a dependency are outside the project and are not linked. Asset contents are measured, not looked at: view an image to see it. So "used by nothing" or "named by no file" means nothing the map found, not that nothing uses it.

The map is a guide to where to look, not a replacement for reading the files: view a definition before you edit it. It updates itself on every call, re-reading only the files that changed. For type-exact answers in one language, code_query asks the language server when one is configured.

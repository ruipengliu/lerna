# Exact Content contract 1.2.0

This isolated entry implements closed `content.put`, `content.get`, and Content
`command.get` types/codecs. Its three methods are declared and unadvertised until
the whole Content profile exits. It does not add a Decision or Task contract.
The machine source is `contract/schema/1.2.0/values.json`; Go and TypeScript
outputs come from the explicit 1.2 generator configuration.

Put accepts canonical padded RFC4648 base64, at most 256 KiB decoded bytes,
64 unique exact source versions, and an absolute requested retention cap. The
original version, declaration, command subject, and fixed receipt do not change
on retry. `accepted` confirms durable publication responsibility; it does not
confirm object readability. Its `retain_until` is the first effective upper cap,
not a guarantee of continued permission or preservation.

Content get requires current exact scope and both read and disclose permission.
It returns no preparing body. Published range reads verify the complete bounded
object before slicing; `DecodeContentResponse` also binds the exact request ref
and range. Missing and damaged objects are unavailable, never another version.
History in Command progress does not grant current readability. Old readers
cannot losslessly represent the new accepted receipt and must return unavailable.

Strict decoding rejects unknown/duplicate fields, numeric quantities, invalid
Unicode, noncanonical base64, and invalid ranges/responses. Neither caller purpose
nor a ContentRef is an authorization. All policy installation is a separate
trusted fixture seam, not a production Grant or an open client method.

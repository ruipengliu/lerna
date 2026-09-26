import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { createHash } from "node:crypto";
import { FileBlob, Presentation, PresentationFile } from "@oai/artifact-tool";

const BUILD_DIR = path.dirname(fileURLToPath(import.meta.url));
const WORKSPACE_DIR = path.resolve(BUILD_DIR, "../../..");
const OUTPUT_DIR = path.join(WORKSPACE_DIR, "artifacts/harness-architecture-review-2026-09-26");
const SLIDES_DIR = path.join(OUTPUT_DIR, "slides");
const SKILL_DIR = "/Users/ruipengliu/.codex/plugins/cache/openai-primary-runtime/presentations/26.909.11814/skills/presentations";
const RUNTIME_PYTHON = "/Users/ruipengliu/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/bin/python3";
process.env.RUNTIME_NODE_MODULES ??= "/Users/ruipengliu/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules";
process.env.RUNTIME_NODE ??= "/Users/ruipengliu/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node";
const CANDIDATE_PATH = path.join(BUILD_DIR, "candidate.pptx");
const FINAL_PATH = process.env.FINAL_PPTX
  ? path.resolve(process.env.FINAL_PPTX)
  : path.join(OUTPUT_DIR, "Harness-架构评审.pptx");
const RENDER_DIR = path.join(BUILD_DIR, "rendered");
const WIDTH = 1280;
const HEIGHT = 720;
const EXPECTED_SLIDE_COUNT = 14;

function invariant(condition, message) {
  if (!condition) throw new Error(message);
}

function flattenText(value) {
  if (value === undefined || value === null) return "";
  if (typeof value === "string") return value;
  if (Array.isArray(value)) return value.map(flattenText).filter(Boolean).join("\n");
  if (typeof value === "object") {
    return Object.entries(value)
      .map(([key, item]) => `${key}: ${flattenText(item)}`)
      .join("\n");
  }
  return String(value);
}

function notesFor(entry, index) {
  const sections = [`第 ${index + 1} 页：${entry.title}`];
  const copy = flattenText(entry.copy).trim();
  const notes = flattenText(entry.notes).trim();
  const sources = flattenText(entry.sources).trim();
  if (copy) sections.push(`页面文案\n${copy}`);
  if (notes) sections.push(`讲稿\n${notes}`);
  if (sources) sections.push(`资料来源\n${sources}`);
  return sections.join("\n\n");
}

function pngSize(bytes, filePath) {
  const signature = Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]);
  invariant(bytes.length >= 24 && bytes.subarray(0, 8).equals(signature), `Expected a PNG: ${filePath}`);
  invariant(bytes.toString("ascii", 12, 16) === "IHDR", `Missing PNG IHDR: ${filePath}`);
  return { width: bytes.readUInt32BE(16), height: bytes.readUInt32BE(20) };
}

async function main() {
  invariant(FINAL_PATH.startsWith(`${WORKSPACE_DIR}${path.sep}`), "Final PPTX must be inside the workspace.");
  invariant(path.dirname(FINAL_PATH) !== BUILD_DIR, "Final PPTX must be separate from the private build directory.");
  await fs.mkdir(RENDER_DIR, { recursive: true });
  await fs.mkdir(path.dirname(FINAL_PATH), { recursive: true });
  try {
    await fs.access(FINAL_PATH);
    throw new Error(`Final output already exists. Set FINAL_PPTX to a new filename: ${FINAL_PATH}`);
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }

  const metadata = JSON.parse(await fs.readFile(path.join(BUILD_DIR, "deck.json"), "utf8"));
  invariant(Array.isArray(metadata.slides), "deck.json must contain a slides array.");
  invariant(metadata.slides.length === EXPECTED_SLIDE_COUNT, `Expected ${EXPECTED_SLIDE_COUNT} slides; received ${metadata.slides.length}.`);

  // Read and verify all inputs before authoring the deck.
  const imageInputs = [];
  const manifest = [];
  for (const [index, entry] of metadata.slides.entries()) {
    const pageNumber = index + 1;
    const basename = `slide-${String(pageNumber).padStart(2, "0")}`;
    const imagePath = path.join(SLIDES_DIR, `${basename}.png`);
    invariant(typeof entry.title === "string" && entry.title.trim(), `${basename} needs a title.`);
    invariant(flattenText(entry.copy).trim(), `${basename} needs page copy.`);
    invariant(flattenText(entry.notes).trim(), `${basename} needs speaker notes.`);
    invariant(flattenText(entry.sources).trim(), `${basename} needs sources.`);
    const bytes = await fs.readFile(imagePath);
    const dimensions = pngSize(bytes, imagePath);
    const ratioError = Math.abs((dimensions.width / dimensions.height) / (WIDTH / HEIGHT) - 1);
    if (ratioError > 0.005) {
      console.warn(`${basename}: source is ${dimensions.width}×${dimensions.height}; contain fit preserves the full image within the 16:9 canvas.`);
    }
    const notes = notesFor(entry, index);
    imageInputs.push({ bytes, basename, entry, notes });
    manifest.push({
      slide: pageNumber,
      id: entry.id,
      title: entry.title,
      imagePath,
      ...dimensions,
      ratioError,
      sha256: createHash("sha256").update(bytes).digest("hex"),
      notes,
    });
  }

  const presentation = Presentation.create({ slideSize: { width: WIDTH, height: HEIGHT } });
  for (const { bytes, entry, notes } of imageInputs) {
    const slide = presentation.slides.add();
    slide.background.fill = "#FFFFFF";
    slide.images.add({
      blob: new Uint8Array(bytes),
      contentType: "image/png",
      alt: `${entry.title}\n${flattenText(entry.copy)}`,
      fit: "contain",
      position: { left: 0, top: 0, width: WIDTH, height: HEIGHT },
    });
    slide.speakerNotes.textFrame.setText(notes);
  }
  await fs.writeFile(path.join(BUILD_DIR, "source-manifest.json"), `${JSON.stringify(manifest, null, 2)}\n`);
  await (await PresentationFile.exportPptx(presentation)).save(CANDIDATE_PATH);
  console.log(`Candidate: ${CANDIDATE_PATH}`);

  const { finalizePresentation } = await import(pathToFileURL(
    path.join(SKILL_DIR, "container_tools/artifact_tool_utils.mjs"),
  ).href);
  const result = await finalizePresentation({
    workspaceDir: WORKSPACE_DIR,
    candidatePath: CANDIDATE_PATH,
    finalPath: FINAL_PATH,
    pythonExecutable: RUNTIME_PYTHON,
    integrityValidatorPath: path.join(SKILL_DIR, "container_tools/inspect_presentation_package_integrity.py"),
    layoutValidatorPath: path.join(SKILL_DIR, "container_tools/inspect_presentation_layout_geometry.py"),
    layoutArgs: [
      "--expected-slide-size-emu", "12192000,6858000",
      "--validate-bullet-geometry",
      "--validate-heading-fit",
    ],
    // The user explicitly requested imagegen pages, so the evidence is part of
    // each generated bitmap. The exact copy and sources remain in the notes.
    requiredNativeTableOwnerSlides: [],
    requiredNativeChartOwnerSlides: [],
    verifyArtifactToolImport: true,
    receiptPath: path.join(BUILD_DIR, "presentation.validation.json"),
  });
  await fs.writeFile(path.join(BUILD_DIR, "finalization-result.json"), `${JSON.stringify(result, null, 2)}\n`);
  console.log(`Final: ${FINAL_PATH}`);

  // Render the final package, rather than only the authoring object.
  const finalPresentation = await PresentationFile.importPptx(await FileBlob.load(FINAL_PATH));
  invariant(finalPresentation.slides.items.length === EXPECTED_SLIDE_COUNT,
    `Final package has ${finalPresentation.slides.items.length} slides.`);
  for (const [index, slide] of finalPresentation.slides.items.entries()) {
    const basename = `slide-${String(index + 1).padStart(2, "0")}`;
    const rendered = await finalPresentation.export({ slide, format: "png", scale: 1.5 });
    await fs.writeFile(path.join(RENDER_DIR, `${basename}.png`), new Uint8Array(await rendered.arrayBuffer()));
    const layout = await slide.export({ format: "layout" });
    await fs.writeFile(path.join(RENDER_DIR, `${basename}.layout.json`), await layout.text());
    console.log(`Rendered ${basename}`);
  }
  const montage = await finalPresentation.export({ format: "webp", montage: true, scale: 0.5 });
  await fs.writeFile(path.join(BUILD_DIR, "contact-sheet.webp"), new Uint8Array(await montage.arrayBuffer()));
  console.log(`Render directory: ${RENDER_DIR}`);
}

await main();

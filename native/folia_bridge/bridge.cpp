// gbwater 的文章 HTML writer：只使用 Folia 的 Markdown 解析结果（folia.markdown），
// 不依赖 folia_core / exporter。数学与 Mermaid 先以占位元素输出，后续接入 SVG。
import std;
import folia.markdown;
import folia.markdown.image_dimension;
import folia.markdown.utf;
import folia.markdown.block_source;

namespace {

using folia::BlockKind;
using folia::InlineCstKind;
using folia::InlineDocument;
using folia::BlockNode;

std::string escape_text(std::string_view value) {
    std::string out;
    out.reserve(value.size());
    for (char c : value) {
        switch (c) {
            case '&': out += "&amp;"; break;
            case '<': out += "&lt;"; break;
            case '>': out += "&gt;"; break;
            case '"': out += "&quot;"; break;
            default: out.push_back(c); break;
        }
    }
    return out;
}

std::string inline_html(const InlineDocument& document, const folia::InlineCstNodes& nodes);

std::string image_attrs(const std::optional<folia::ImageDimension>& width,
                        const std::optional<folia::ImageDimension>& height) {
    std::string attrs;
    auto append = [&](std::string_view name, const folia::ImageDimension& dimension) {
        attrs += " " + std::string(name) + "=\"" + std::to_string(dimension.value);
        if (dimension.unit == folia::ImageDimensionUnit::Percent) attrs += '%';
        attrs += '"';
    };
    if (width) append("width", *width);
    if (height) append("height", *height);
    return attrs;
}

std::string inline_html(const InlineDocument& document, const folia::InlineCstNodes& nodes) {
    std::string html;
    for (const auto& node : nodes) {
        const auto raw = folia::cps_to_utf8(folia::inline_source_slice(document, node.range));
        switch (node.kind) {
            case InlineCstKind::Strong:
                html += "<strong>" + inline_html(document, node.children) + "</strong>";
                break;
            case InlineCstKind::Emphasis:
                html += "<em>" + inline_html(document, node.children) + "</em>";
                break;
            case InlineCstKind::Highlight: {
                html += "<mark class=\"markdown-highlight\">" + inline_html(document, node.children) + "</mark>";
                break;
            }
            case InlineCstKind::Strikethrough:
                html += "<del>" + inline_html(document, node.children) + "</del>";
                break;
            case InlineCstKind::CodeSpan:
                html += "<code>" + escape_text(folia::cps_to_utf8(
                    folia::inline_source_slice(document, node.delimiter_ranges().content))) + "</code>";
                break;
            case InlineCstKind::InlineMath:
                html += "<span class=\"math-inline\">" + escape_text(folia::cps_to_utf8(
                    folia::inline_source_slice(document, node.delimiter_ranges().content))) + "</span>";
                break;
            case InlineCstKind::Link: {
                const auto& semantic = node.semantics();
                const auto title = semantic.title ? " title=\"" + escape_text(*semantic.title) + "\"" : std::string{};
                html += "<a href=\"" + escape_text(semantic.href) + "\"" + title + ">"
                    + inline_html(document, node.children) + "</a>";
                break;
            }
            case InlineCstKind::Image: {
                const auto& semantic = node.semantics();
                const auto title = semantic.title ? " title=\"" + escape_text(*semantic.title) + "\"" : std::string{};
                html += "<img src=\"" + escape_text(semantic.href) + "\" alt=\"" + escape_text(semantic.alt) + "\""
                    + title + image_attrs(semantic.image_width, semantic.image_height) + " loading=\"lazy\">";
                break;
            }
            case InlineCstKind::HtmlElement: {
                const auto& semantic = node.semantics();
                const auto& tag = semantic.html_tag;
                if (tag == "br") { html += "<br>\n"; break; }
                if (tag == "img") {
                    html += "<img src=\"" + escape_text(semantic.href) + "\" alt=\""
                        + escape_text(semantic.alt) + "\"" + image_attrs(semantic.image_width, semantic.image_height) + ">";
                    break;
                }
                const auto content = inline_html(document, node.children);
                if (tag == "a") {
                    html += "<a href=\"" + escape_text(semantic.href) + "\">" + content + "</a>";
                } else if (tag == "strong" || tag == "em" || tag == "code" || tag == "del"
                    || tag == "span" || tag == "mark" || tag == "sup" || tag == "sub") {
                    html += "<" + tag + ">" + content + "</" + tag + ">";
                } else {
                    html += content;
                }
                break;
            }
            case InlineCstKind::HtmlComment:
                break;
            case InlineCstKind::Autolink:
                html += "<a href=\"" + escape_text(node.semantics().href) + "\">"
                    + escape_text(folia::cps_to_utf8(
                        folia::inline_source_slice(document, node.delimiter_ranges().content))) + "</a>";
                break;
            case InlineCstKind::FootnoteRef:
                html += "<sup class=\"footnote-ref\">" + escape_text(node.semantics().label) + "</sup>";
                break;
            case InlineCstKind::WikiLink:
                html += escape_text(node.semantics().alias.value_or(node.semantics().target));
                break;
            case InlineCstKind::Incomplete: {
                // Folia keeps unfinished delimiters for editing. Publish their
                // parsed children so complete math and formatting still render.
                if (!node.children.empty()) {
                    html += inline_html(document, node.children);
                } else {
                    const auto& ranges = node.delimiter_ranges();
                    const auto opening = folia::inline_source_slice(document, ranges.opening);
                    if (opening == U"$" || opening == U"\\(") {
                        throw std::runtime_error("行内公式缺少闭合符，请将行内公式写在同一行");
                    } else {
                        html += escape_text(folia::cps_to_utf8(folia::inline_source_slice(document, ranges.content)));
                    }
                }
                break;
            }
            case InlineCstKind::SoftBreak:
            case InlineCstKind::HardBreak:
                html += "<br>\n";
                break;
            case InlineCstKind::Escape: {
                const auto source = folia::inline_source_slice(document, node.range);
                html += escape_text(folia::cps_to_utf8(source.empty() ? source : source.substr(1)));
                break;
            }
            case InlineCstKind::Entity:
                html += escape_text(folia::cps_to_utf8(
                    folia::decode_inline_entity(folia::inline_source_slice(document, node.range))));
                break;
            case InlineCstKind::Raw:
                html += escape_text(raw);
                break;
            default:
                html += escape_text(raw);
                break;
        }
    }
    return html;
}

std::string inline_html(const folia::InlineDocument& document) {
    return inline_html(document, document.tree.nodes);
}

std::string blocks_html(const folia::BlockVec& blocks);

std::string block_html(const BlockNode& block) {
    using BK = BlockKind;
    switch (block.kind) {
        case BK::Heading: {
            const auto& special = block.text_special();
            const auto level = std::min<std::uint8_t>(special.level == 0 ? 2 : special.level, 6);
            const auto tag = "h" + std::to_string(level);
            return "<" + tag + " id=\"" + escape_text(special.slug) + "\">"
                + inline_html(block.inline_content) + "</" + tag + ">\n";
        }
        case BK::Paragraph: {
            const auto content = inline_html(block.inline_content);
            if (content.empty()) return {};
            return "<p>" + content + "</p>\n";
        }
        case BK::CodeBlock: {
            std::string language;
            if (block.block_source.tree().language) language = *block.block_source.tree().language;
            const auto content = folia::cps_to_utf8(folia::block_source_content(block.block_source));
            auto diagram_language = language.substr(0, language.find_first_of(" \t"));
            std::ranges::transform(diagram_language, diagram_language.begin(),
                [](unsigned char ch) { return static_cast<char>(std::tolower(ch)); });
            if (diagram_language == "mermaid") {
                return "<pre class=\"mermaid-source\"><code>" + escape_text(content) + "</code></pre>\n";
            }
            const auto attr = language.empty() ? std::string{} : " class=\"language-" + escape_text(language) + "\"";
            return "<pre><code" + attr + ">" + escape_text(content) + "</code></pre>\n";
        }
        case BK::MathBlock: {
            const auto content = folia::cps_to_utf8(folia::block_source_content(block.block_source));
            return "<div class=\"math-block\">" + escape_text(content) + "</div>\n";
        }
        case BK::BlockQuote:
            return "<blockquote>\n" + blocks_html(block.children) + "</blockquote>\n";
        case BK::List:
        case BK::TaskList: {
            const auto& special = block.list_special();
            std::string items;
            bool tasks = block.kind == BK::TaskList;
            for (const auto& item : block.children) {
                const bool task = item.kind == BK::TaskListItem;
                tasks = tasks || task;
                items += "<li>";
                if (task) {
                    items += "<input type=\"checkbox\"";
                    if (item.item_special().checked) items += " checked=\"\"";
                    items += " disabled=\"\"> ";
                }
                for (const auto& child : item.children) {
                    if (special.tight && child.kind == BK::Paragraph) {
                        items += inline_html(child.inline_content);
                    } else {
                        items += block_html(child);
                    }
                }
                items += "</li>\n";
            }
            if (special.ordered) {
                const auto start = special.start == 1 ? std::string{} : " start=\"" + std::to_string(special.start) + "\"";
                return "<ol" + start + ">\n" + items + "</ol>\n";
            }
            return std::string("<ul") + (tasks ? " class=\"task-list\"" : "") + ">\n" + items + "</ul>\n";
        }
        case BK::Table: {
            std::string html = "<table>\n";
            for (const auto& row : block.children) {
                const auto tag = row.table_special().table_header_row ? "th" : "td";
                html += "<tr>";
                for (const auto& cell : row.children) {
                    html += "<" + std::string(tag) + ">" + inline_html(cell.inline_content) + "</" + std::string(tag) + ">";
                }
                html += "</tr>\n";
            }
            return html + "</table>\n";
        }
        case BK::ThematicBreak:
            return "<hr>\n";
        case BK::Callout: {
            auto kind = block.container_special().callout_kind;
            std::ranges::transform(kind, kind.begin(), [](unsigned char ch) { return static_cast<char>(std::tolower(ch)); });
            std::string inner;
            for (const auto& child : block.children) {
                if (child.kind == BK::CalloutTitle) continue;
                inner += block_html(child);
            }
            if (kind == "center") return "<div class=\"content-center\">\n" + inner + "</div>\n";
            if (kind == "minipage") return "<section class=\"minipage\">\n" + inner + "</section>\n";
            std::string label;
            if (kind == "proof") label = "证明";
            else if (kind == "theorem") label = "定理";
            else if (kind == "definition") label = "定义";
            else if (kind == "example") label = "示例";
            else if (kind == "bbox") label = "要点";
            else if (kind == "warning") label = "注意";
            else if (kind == "note") label = "注记";
            else label = kind;
            return "<section class=\"callout callout-" + escape_text(kind) + "\">\n<div class=\"callout-label\">"
                + escape_text(label) + "</div>\n" + inner + "</section>\n";
        }
        case BK::CalloutTitle:
            return "<div class=\"callout-label\">" + inline_html(block.inline_content) + "</div>\n";
        case BK::FootnoteDefinition: {
            const auto& special = block.container_special();
            return "<section class=\"footnote\" id=\"fn-" + escape_text(special.footnote_label) + "\">\n"
                + blocks_html(block.children) + "</section>\n";
        }
        case BK::ImageBlock: {
            const auto& special = block.image_special();
            const auto title = special.image_title ? " title=\"" + escape_text(*special.image_title) + "\"" : std::string{};
            auto image = "<img src=\"" + escape_text(special.src) + "\" alt=\"" + escape_text(special.image_alt)
                + "\"" + title + image_attrs(special.image_width, special.image_height) + " loading=\"lazy\">";
            if (special.image_link) image = "<a href=\"" + escape_text(*special.image_link) + "\">" + image + "</a>";
            return "<p>" + image + "</p>\n";
        }
        case BK::Frontmatter:
        case BK::LinkDefinition:
        case BK::Toc:
            return {};
        case BK::HtmlContainer:
        case BK::Document:
        case BK::ListItem:
        case BK::TaskListItem:
            return blocks_html(block.children);
        case BK::TableRow:
        case BK::TableCell:
            return {};
        case BK::UnsupportedMarkup:
        case BK::Extension:
            return {};
    }
    return {};
}

std::string blocks_html(const folia::BlockVec& blocks) {
    std::string html;
    for (const auto& block : blocks) html += block_html(block);
    return html;
}

} // namespace

// 站点内容使用 Pandoc 风格 ::: 容器；Folia 解析器只认 GFM callout，
// 这里先转成 > [!KIND] 引用块，writer 再映射回 callout/center/minipage。
std::string preprocess_containers(std::string_view source) {
    std::vector<std::string> lines;
    std::string current;
    for (char ch : source) {
        if (ch == '\n') { lines.push_back(current); current.clear(); }
        else if (ch != '\r') current.push_back(ch);
    }
    if (!current.empty()) lines.push_back(current);
    std::vector<std::string> stack;
    std::string out;
    auto prefix = [](std::size_t depth) {
        std::string value;
        for (std::size_t i = 0; i < depth; ++i) value += "> ";
        return value;
    };
    for (const auto& line : lines) {
        if (line.rfind(":::", 0) == 0) {
            auto name = line.substr(3);
            const auto begin = name.find_first_not_of(" \t");
            const auto end = name.find_last_not_of(" \t");
            name = begin == std::string::npos ? std::string{} : name.substr(begin, end - begin + 1);
            if (name.empty()) {
                if (!stack.empty()) stack.pop_back();
                continue;
            }
            const auto space = name.find_first_of(" \t");
            if (space != std::string::npos) name = name.substr(0, space);
            std::ranges::transform(name, name.begin(), [](unsigned char ch) { return static_cast<char>(std::toupper(ch)); });
            stack.push_back(name);
            out += prefix(stack.size()) + "[!" + name + "]\n";
            continue;
        }
        if (stack.empty()) {
            out += line + "\n";
        } else if (line.empty()) {
            for (std::size_t i = 0; i < stack.size(); ++i) out += ">";
            out += "\n";
        } else {
            out += prefix(stack.size()) + line + "\n";
        }
    }
    return out;
}
int main(int argc, char** argv) {
    if (argc < 2) {
        std::cerr << "usage: folia_md <markdown-file>\n";
        return 2;
    }
    std::ifstream input(argv[1], std::ios::binary);
    if (!input) {
        std::cerr << "cannot open " << argv[1] << "\n";
        return 1;
    }
    std::string source((std::istreambuf_iterator<char>(input)), std::istreambuf_iterator<char>());
    try {
        auto parsed = folia::parse_text(1, source);
        std::cout << blocks_html(parsed.document.root.children);
    } catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
    return 0;
}

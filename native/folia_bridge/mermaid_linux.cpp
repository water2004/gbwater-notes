#include <fontconfig/fontconfig.h>
#include <pango/pangocairo.h>

import std;
import folia.mermaid;
import folia.platform.mermaid_math;

namespace {
using namespace folia::mermaid;
using Layout = std::unique_ptr<PangoLayout, decltype(&g_object_unref)>;

std::string utf8(std::u16string_view value) {
    if (value.empty()) return {};
    const std::vector<gunichar2> units(value.begin(), value.end());
    glong length = 0;
    std::unique_ptr<char, decltype(&g_free)> result{
        g_utf16_to_utf8(units.data(), units.size(), nullptr, &length, nullptr), g_free};
    if (!result) throw std::runtime_error("Invalid Mermaid UTF-16 text");
    return {result.get(), static_cast<std::size_t>(length)};
}

std::u16string utf16(std::string_view value) {
    if (value.empty()) return {};
    glong length = 0;
    std::unique_ptr<gunichar2, decltype(&g_free)> result{
        g_utf8_to_utf16(value.data(), value.size(), nullptr, &length, nullptr), g_free};
    if (!result) throw std::runtime_error("Invalid Mermaid UTF-8 text");
    return {result.get(), result.get() + length};
}

std::u16string collapse_spaces(std::u16string_view source) {
    std::u16string text;
    bool space = false;
    for (const auto character : source) {
        if (character == u' ' || character == u'\t' || character == u'\n' || character == u'\r') {
            space = !text.empty();
        } else {
            if (space) text += u' ';
            text += character;
            space = false;
        }
    }
    return text;
}

Layout shape(std::u16string_view text, const MermaidFont& font, std::span<const MermaidTextStyle> styles) {
    if (font.family.empty() || !std::isfinite(font.size) || font.size <= 0)
        throw std::runtime_error("Invalid Mermaid font");
    const auto source = utf8(text);
    const auto family = utf8(font.family);
    if (source.size() > static_cast<std::size_t>(std::numeric_limits<int>::max()))
        throw std::runtime_error("Mermaid text is too large");
    std::unique_ptr<PangoContext, decltype(&g_object_unref)> context{
        pango_font_map_create_context(pango_cairo_font_map_get_default()), g_object_unref};
    Layout layout{pango_layout_new(context.get()), g_object_unref};
    std::unique_ptr<PangoFontDescription, decltype(&pango_font_description_free)> description{
        pango_font_description_new(), pango_font_description_free};
    pango_font_description_set_family(description.get(), family.c_str());
    pango_font_description_set_absolute_size(description.get(), font.size * PANGO_SCALE);
    pango_font_description_set_weight(description.get(), static_cast<PangoWeight>(font.weight));
    pango_font_description_set_style(description.get(), font.italic ? PANGO_STYLE_ITALIC : PANGO_STYLE_NORMAL);
    pango_layout_set_font_description(layout.get(), description.get());
    pango_layout_set_text(layout.get(), source.data(), static_cast<int>(source.size()));

    std::unique_ptr<PangoAttrList, decltype(&pango_attr_list_unref)> attrs{
        pango_attr_list_new(), pango_attr_list_unref};
    if (!font.pair_kerning) pango_attr_list_insert(attrs.get(), pango_attr_font_features_new("kern=0"));
    for (const auto& style : styles) {
        if (style.offset > text.size() || style.length > text.size() - style.offset)
            throw std::runtime_error("Invalid Mermaid text style range");
        const auto start = utf8(text.substr(0, style.offset)).size();
        const auto end = start + utf8(text.substr(style.offset, style.length)).size();
        for (auto* attribute : {pango_attr_weight_new(static_cast<PangoWeight>(style.weight)),
                                pango_attr_style_new(style.italic ? PANGO_STYLE_ITALIC : PANGO_STYLE_NORMAL)}) {
            attribute->start_index = static_cast<unsigned int>(start);
            attribute->end_index = static_cast<unsigned int>(end);
            pango_attr_list_insert(attrs.get(), attribute);
        }
    }
    pango_layout_set_attributes(layout.get(), attrs.get());
    return layout;
}

double pixels(int units) { return static_cast<double>(units) / PANGO_SCALE; }

class PangoMeasurer final : public TextMeasurer {
public:
    std::expected<std::u16string, std::string> resolve_family(
        std::u16string_view families, MermaidFontContext) const override {
        std::unique_ptr<FcPattern, decltype(&FcPatternDestroy)> pattern{FcPatternCreate(), FcPatternDestroy};
        for (const auto range : utf8(families) | std::views::split(',')) {
            const std::string value(range.begin(), range.end());
            const auto first = value.find_first_not_of(" \t\r\n'\"");
            if (first == value.npos) continue;
            const auto name = value.substr(first, value.find_last_not_of(" \t\r\n'\"") - first + 1);
            FcPatternAddString(pattern.get(), FC_FAMILY, reinterpret_cast<const FcChar8*>(name.c_str()));
        }
        FcConfigSubstitute(nullptr, pattern.get(), FcMatchPattern);
        FcDefaultSubstitute(pattern.get());
        FcResult result;
        std::unique_ptr<FcPattern, decltype(&FcPatternDestroy)> match{
            FcFontMatch(nullptr, pattern.get(), &result), FcPatternDestroy};
        FcChar8* family = nullptr;
        if (!match || FcPatternGetString(match.get(), FC_FAMILY, 0, &family) != FcResultMatch)
            return std::unexpected("Fontconfig could not resolve the Mermaid font");
        return utf16(reinterpret_cast<const char*>(family));
    }

    std::expected<MermaidTextMetrics, std::string> measure(
        std::u16string_view source, const MermaidFont& font, bool preserve_spaces,
        std::span<const MermaidTextStyle> styles, bool) const override {
        if (!preserve_spaces && !styles.empty()) return std::unexpected("Styled Mermaid text must preserve spaces");
        const auto text = preserve_spaces ? std::u16string(source) : collapse_spaces(source);
        if (text.empty()) return MermaidTextMetrics{};
        auto layout = shape(text, font, styles);
        PangoRectangle ink{}, logical{};
        pango_layout_get_extents(layout.get(), &ink, &logical);
        const int left = std::min(0, ink.x);
        const int right = std::max(logical.width, ink.x + ink.width);
        return MermaidTextMetrics{pixels(logical.width), pixels(ink.width), pixels(right - left),
                                  pixels(logical.height), pixels(pango_layout_get_baseline(layout.get()))};
    }

    std::expected<std::vector<std::u16string>, std::string> wrap(
        std::u16string_view source, const MermaidFont& font, double width,
        std::span<const MermaidTextStyle> styles, bool break_spaces) const override {
        if (width == std::numeric_limits<double>::infinity())
            return std::vector<std::u16string>{std::u16string(source)};
        if (!std::isfinite(width) || width <= 0) return std::unexpected("Invalid Mermaid wrapping width");
        const auto raw = utf8(source);
        auto layout = shape(source, font, styles);
        pango_layout_set_width(layout.get(), static_cast<int>(std::clamp(
            width * PANGO_SCALE, 1.0, static_cast<double>(std::numeric_limits<int>::max()))));
        pango_layout_set_wrap(layout.get(), break_spaces ? PANGO_WRAP_WORD_CHAR : PANGO_WRAP_WORD);
        std::vector<std::u16string> result;
        for (auto* item = pango_layout_get_lines_readonly(layout.get()); item; item = item->next) {
            auto* line = static_cast<PangoLayoutLine*>(item->data);
            result.push_back(utf16(std::string_view(raw).substr(line->start_index, line->length)));
        }
        return result;
    }

    std::expected<MermaidSvgFontMetrics, std::string> svg_font_metrics(const MermaidFont& font) const override {
        auto layout = shape(u"x", font, {});
        PangoRectangle ink{}, logical{};
        pango_layout_get_extents(layout.get(), &ink, &logical);
        return MermaidSvgFontMetrics{pixels(logical.height),
            pixels(pango_layout_get_baseline(layout.get())), pixels(ink.height) / 2};
    }
};

std::expected<DiagramSvg, std::string> render(std::string_view source, bool dark) {
    auto session = ConfigSession::create();
    if (!session) return std::unexpected(session.error());
    ConfigValue config{.kind = ConfigKind::mapping};
    config.mapping.emplace(u"theme", ConfigValue{.kind = ConfigKind::string, .string = dark ? u"dark" : u"default"});
    config.mapping.emplace(u"look", ConfigValue{.kind = ConfigKind::string, .string = u"classic"});
    if (auto configured = session->initialize(config); !configured) return std::unexpected(configured.error());
    PangoMeasurer measurer;
    folia::platform::MermaidMathRenderer math;
    return render_diagram(utf16(source), measurer, *session,
        [&](std::u16string_view tex, double size) { return math.render(tex, size); }, {}, 1120);
}

void set_output(std::string_view value, std::uint8_t** output, std::size_t* length) {
    auto bytes = std::make_unique<std::uint8_t[]>(value.size());
    std::memcpy(bytes.get(), value.data(), value.size());
    *length = value.size();
    *output = bytes.release();
}
}

extern "C" [[gnu::visibility("default")]] std::int32_t FoliaNativeMermaidRender(
    const std::uint8_t* source, std::size_t size, bool dark, std::uint8_t** output,
    std::size_t* length, float* width, float* height) noexcept {
    if (!source || !output || !length || !width || !height) return -1;
    *output = nullptr;
    *length = 0;
    *width = 0;
    *height = 0;
    try {
        auto diagram = render({reinterpret_cast<const char*>(source), size}, dark);
        if (!diagram) {
            set_output(diagram.error(), output, length);
            return 1;
        }
        set_output(diagram->svg, output, length);
        *width = static_cast<float>(diagram->width);
        *height = static_cast<float>(diagram->height);
        return 0;
    } catch (...) {
        return 2;
    }
}

extern "C" [[gnu::visibility("default")]] void FoliaNativeMermaidFree(std::uint8_t* output) noexcept {
    delete[] output;
}

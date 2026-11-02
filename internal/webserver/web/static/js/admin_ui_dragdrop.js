// Replace Dropzone's presentation callbacks, keeping its file admission and
// upload handling intact. Enter/leave events also fire for nested artwork/text.
function createUploadDragFeedback(dropzone) {
    const zone = dropzone.element;
    const trigger = document.getElementById('selectUploadFiles');
    const title = document.getElementById('uploadBoxTitle');
    const subtitle = document.getElementById('uploadBoxSubtitle');
    let depth = 0, previous = null;

    function reset() {
        depth = 0;
        if (!previous) return;
        zone.classList.remove('is-file-dragover');
        title.textContent = previous.title;
        subtitle.textContent = previous.subtitle;
        previous = null;
    }

    function ready() {
        return !dropzone.disabled && !trigger.disabled;
    }

    function activate(event) {
        // During an OS drag the files collection may be protected and empty.
        const types = Array.from(event.dataTransfer?.types || []);
        if (!types.includes('Files') || !ready()) {
            reset();
            return false;
        }
        if (!previous) {
            // E2E readiness can change this copy after Dropzone initialization.
            previous = {title: title.textContent, subtitle: subtitle.textContent};
            title.textContent = t('files.dropToUpload');
            subtitle.textContent = t('files.dropUsesRules');
            zone.classList.add('is-file-dragover');
        }
        return true;
    }

    // Drop elsewhere or cancel the native drag without leaving stale feedback.
    document.addEventListener('drop', reset, true);
    document.addEventListener('dragend', reset, true);
    document.addEventListener('keydown', event => { if (event.key === 'Escape') reset(); });
    window.addEventListener('blur', reset);
    const availability = new MutationObserver(() => { if (!ready()) reset(); });
    availability.observe(trigger, {attributes: true, attributeFilter: ['disabled']});
    availability.observe(zone, {attributes: true, attributeFilter: ['class']});

    return {
        enter(event) { if (activate(event)) depth++; },
        over(event) {
            if (!activate(event)) {
                if (event.dataTransfer) event.dataTransfer.dropEffect = 'none';
                return;
            }
            depth = Math.max(depth, 1);
            event.dataTransfer.dropEffect = 'copy';
        },
        leave() {
            depth = Math.max(0, depth - 1);
            if (!depth) reset();
        },
        reset
    };
}

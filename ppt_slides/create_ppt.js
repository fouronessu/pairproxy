const pptxgen = require('pptxgenjs');
const html2pptx = require('C:/Users/HW/.agents/skills/pptx/scripts/html2pptx');
const path = require('path');

async function createPresentation() {
    const pptx = new pptxgen();
    pptx.layout = 'LAYOUT_16x9';
    pptx.author = 'PairProxy Team';
    pptx.title = 'PairProxy - 企业级 LLM API 代理网关';
    pptx.subject = '管理例会汇报';
    pptx.company = 'AI Coding Development';

    const slidesDir = 'D:\\pairproxy\\ppt_slides';
    
    // Slide files in order
    const slideFiles = [
        'slide_01_title.html',
        'slide_02_pain_points.html',
        'slide_03_architecture.html',
        'slide_04_capabilities.html',
        'slide_05_value.html',
        'slide_06_ai_efficiency.html',
        'slide_07_production.html',
        'slide_08_deployment.html',
        'slide_09_future.html',
        'slide_10_end.html'
    ];

    console.log('Creating PairProxy Presentation...');
    console.log('================================');

    for (let i = 0; i < slideFiles.length; i++) {
        const file = slideFiles[i];
        const filePath = path.join(slidesDir, file);
        console.log(`Processing slide ${i + 1}/${slideFiles.length}: ${file}`);
        
        try {
            await html2pptx(filePath, pptx);
            console.log(`  ✓ Slide ${i + 1} created`);
        } catch (err) {
            console.error(`  ✗ Error on slide ${i + 1}: ${err.message}`);
            throw err;
        }
    }

    // Save the presentation
    const outputPath = 'D:\\pairproxy\\PairProxy_Management_Presentation.pptx';
    await pptx.writeFile({ fileName: outputPath });
    
    console.log('');
    console.log('================================');
    console.log(`✓ Presentation saved to: ${outputPath}`);
    console.log(`✓ Total slides: ${slideFiles.length}`);
    console.log('================================');
}

createPresentation().catch(err => {
    console.error('Failed to create presentation:', err);
    process.exit(1);
});
